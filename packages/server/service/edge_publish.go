package service

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
)

// The edge configuration - Caddy's site blocks, CoreDNS's zones - follows the
// base domains an organisation has. The API cannot write it: those files are on
// the host, and putting them in service means reloading Caddy and restarting
// CoreDNS, which needs root. This container has neither that nor a Docker
// socket, deliberately.
//
// So the API does the only two things it can. It writes the domain set into the
// agent's inbox, which changes nothing by itself, and it asks the agent to put
// it in service. "The database changed" and "the gateway's edge changed" stay
// two separate events, and the second one is visible.

// PublishEdge records the domain set this organisation should be served on and
// asks the host agent to apply it.
//
// Called whenever the set changes in a way the edge can see: a domain verified,
// its DNS mode switched, a domain removed. Not when one is created - an
// unverified domain is not served, so there is nothing to render for it yet.
func (s *SystemService) PublishEdge(ctx context.Context, orgID, by uuid.UUID) error {
	if s.hostDir() == "" {
		// No host agent configured: a developer running the API locally, where
		// there is no gateway to serve anything.
		return nil
	}
	snap, err := s.edgeSnapshot(ctx, orgID)
	if err != nil {
		return err
	}
	// The tokens before the snapshot that names them, so an apply that finds
	// the snapshot finds them too.
	creds, err := s.edgeCredentials(ctx, *snap)
	if err != nil {
		return err
	}
	if len(creds) > 0 {
		if err := hostagent.WriteDNSCredentials(s.hostDir(), creds); err != nil {
			return fmt.Errorf("hand over the DNS providers' tokens: %w", err)
		}
	}
	if err := hostagent.WriteDesiredEdgeSnapshot(s.hostDir(), *snap); err != nil {
		return fmt.Errorf("record the domain set: %w", err)
	}
	if !s.HostAgentStatus().Reporting {
		// The set is recorded and will be applied the next time anything asks.
		// Not an error: the domain change itself succeeded, and refusing it
		// because a host service is down would be refusing the wrong thing.
		return nil
	}
	return s.queueEdgeApply(by)
}

// SyncEdge publishes the edge when the installed one does not serve the console
// names this binary does: an upgrade to a build that registers one, which
// nothing else would publish until a domain changed. Run once, at start.
func (s *SystemService) SyncEdge(ctx context.Context) {
	if s.hostDir() == "" {
		return
	}
	var primary meshdb.Domain
	if err := s.db.WithContext(ctx).Where("is_primary AND verified").First(&primary).Error; err != nil {
		return
	}
	if err := s.publishNamesIfChanged(ctx, primary.OrganizationID, uuid.Nil); err != nil {
		log.Printf("warning: could not ask the host agent to serve the console names: %v", err)
	}
}

// PublishProjectNames publishes the edge when the projects' console names
// changed: a project created or deleted, where an extension registered a
// prefix. Nothing to do otherwise, so Community never reloads its edge for a
// project.
func (s *SystemService) PublishProjectNames(ctx context.Context, orgID, by uuid.UUID) error {
	if s.hostDir() == "" || len(projectConsolePrefixes) == 0 {
		return nil
	}
	return s.publishNamesIfChanged(ctx, orgID, by)
}

// publishNamesIfChanged publishes the edge when the names it serves as the
// console differ from the installed edge's.
func (s *SystemService) publishNamesIfChanged(ctx context.Context, orgID, by uuid.UUID) error {
	installed, err := hostagent.ReadEdgeSnapshot(s.hostDir())
	if err != nil || installed == nil {
		return err
	}
	want, err := s.edgeSnapshot(ctx, orgID)
	if err != nil {
		return err
	}
	if slices.Equal(installed.ConsoleNames, want.ConsoleNames) &&
		slices.Equal(installed.ProjectConsoleNames, want.ProjectConsoleNames) {
		return nil
	}
	return s.PublishEdge(ctx, orgID, by)
}

// consoleNames are the platform subdomains an extension serves as the
// console, beside console.<domain>, in the order registered.
var consoleNames []string

// RegisterConsoleName serves name.<domain> as the console is, on every domain
// that serves the platform's names, and keeps routes off it. Call it from
// init(), before the server starts; the name must be one plain label. The CE
// binary registers none.
func RegisterConsoleName(name string) {
	for _, n := range consoleNames {
		if n == name {
			return
		}
	}
	consoleNames = append(consoleNames, name)
	platformReservedSubdomains[name] = true
}

// projectConsolePrefixes are the prefixes an extension serves each project
// under as the console: <prefix>.<project slug>.<domain>.
var projectConsolePrefixes []string

// RegisterProjectConsolePrefix serves <prefix>.<project slug>.<domain> as the
// console is, for every project, on every domain that serves the platform's
// names, and keeps routes off those names. Call it from init(), before the
// server starts; the prefix must be one plain label. The CE binary registers
// none.
func RegisterProjectConsolePrefix(prefix string) {
	if !slices.Contains(projectConsolePrefixes, prefix) {
		projectConsolePrefixes = append(projectConsolePrefixes, prefix)
	}
}

// isProjectConsoleName reports whether a route subdomain has the shape of a
// project's console name, <prefix>.<label>, whether or not that project exists:
// one created later would take the name over.
func isProjectConsoleName(subdomain string) bool {
	prefix, rest, ok := strings.Cut(subdomain, ".")
	return ok && !strings.Contains(rest, ".") && slices.Contains(projectConsolePrefixes, prefix)
}

// projectConsoleNames are <prefix>.<slug> for every registered prefix and every
// project of the organisation, environment levels aside: a level is reached
// through its project. A slug that is a domain's internal label is left out,
// as its name would sit inside the mesh-only zone.
func (s *SystemService) projectConsoleNames(ctx context.Context, orgID uuid.UUID, domains []meshdb.Domain) ([]string, error) {
	if len(projectConsolePrefixes) == 0 {
		return nil, nil
	}
	var slugs []string
	if err := s.db.WithContext(ctx).Model(&meshdb.Project{}).
		Where("organization_id = ? AND parent_project_id IS NULL", orgID).
		Order("slug").Pluck("slug", &slugs).Error; err != nil {
		return nil, err
	}
	internal := map[string]bool{"internal": true}
	for _, d := range domains {
		if d.InternalSubdomain != "" {
			internal[d.InternalSubdomain] = true
		}
	}
	var names []string
	for _, prefix := range projectConsolePrefixes {
		for _, slug := range slugs {
			if !internal[slug] {
				names = append(names, prefix+"."+slug)
			}
		}
	}
	return names, nil
}

// IsConsoleName reports whether name is served as the console: "console"
// itself, or a name an extension registered.
func IsConsoleName(name string) bool {
	if name == "console" {
		return true
	}
	for _, n := range consoleNames {
		if n == name {
			return true
		}
	}
	return false
}

// edgeSnapshot turns the domains table into what the generator renders from.
//
// Only verified domains: an unverified one has no DNS pointing here, so a site
// block for it would have Caddy trying for a certificate it cannot get, over
// and over. A retiring domain is still served - retiring stops new routes
// attaching, it does not switch anything off.
func (s *SystemService) edgeSnapshot(ctx context.Context, orgID uuid.UUID) (*hostagent.EdgeSnapshot, error) {
	var domains []meshdb.Domain
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND verified = ?", orgID, true).
		Order("is_primary DESC, created_at ASC").Find(&domains).Error; err != nil {
		return nil, err
	}
	snap := hostagent.EdgeSnapshot{
		PublicIP:     s.cfg.PublicIP,
		MeshIP:       s.cfg.GatewayIP,
		WrittenAt:    time.Now().UTC(),
		ConsoleNames: append([]string(nil), consoleNames...),
	}
	for _, d := range domains {
		mode := string(d.DNSMode)
		if mode == "" {
			// Only a row the startup backfill has not reached yet. The
			// install's mode is the one it was set up in.
			mode = hostagent.EdgeDNSDelegation
			if s.cfg.DNSMode == hostagent.EdgeDNSOnDemand {
				mode = hostagent.EdgeDNSOnDemand
			}
		}
		internal := d.InternalSubdomain
		if internal == "" {
			internal = "internal"
		}
		ed := hostagent.EdgeDomain{
			BaseDomain:        d.BaseDomain,
			InternalSubdomain: internal,
			DNSMode:           mode,
			Primary:           d.IsPrimary,
			ServesPlatform:    d.FormerPrimary,
			Project:           d.ProjectID != nil,
		}
		// A provider means something only on an on-demand domain: a delegated
		// one answers its challenges from the zones the gateway serves.
		if d.DNSProviderID != nil && mode == hostagent.EdgeDNSOnDemand {
			var p meshdb.DNSProvider
			if err := s.db.WithContext(ctx).Select("id", "kind").
				Where("id = ? AND organization_id = ?", *d.DNSProviderID, orgID).First(&p).Error; err != nil {
				return nil, fmt.Errorf("domain %s: its DNS provider: %w", d.BaseDomain, err)
			}
			ed.DNSProvider = &hostagent.EdgeDNSProvider{ID: p.ID.String(), Kind: p.Kind}
		}
		snap.Domains = append(snap.Domains, ed)
	}
	names, err := s.projectConsoleNames(ctx, orgID, domains)
	if err != nil {
		return nil, err
	}
	snap.ProjectConsoleNames = names
	// Node names hang off the install's domain and stay there, whichever
	// domain is primary: moving them would rename every node in the mesh.
	if s.cfg.Domain != "" {
		snap.MeshDomain = "mesh." + s.cfg.Domain
	}
	if err := snap.Validate(); err != nil {
		return nil, fmt.Errorf("this organisation's domains cannot be served: %w", err)
	}
	return &snap, nil
}

// edgeCredentials are the tokens of the DNS providers a snapshot names, for
// the host agent to put where Caddy reads them. Never part of the snapshot,
// which the API and the console read back from state/.
func (s *SystemService) edgeCredentials(ctx context.Context, snap hostagent.EdgeSnapshot) (hostagent.DNSCredentials, error) {
	var ids []string
	for _, d := range snap.Domains {
		if d.DNSProvider != nil {
			ids = append(ids, d.DNSProvider.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []meshdb.DNSProvider
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	creds := hostagent.DNSCredentials{}
	for _, p := range rows {
		creds[p.ID.String()] = string(p.Token)
	}
	return creds, nil
}

// queueEdgeApply leaves a domain.apply request in the inbox.
func (s *SystemService) queueEdgeApply(by uuid.UUID) error {
	return queueHostRequest(s.hostDir(), hostagent.Request{
		ID: uuid.NewString(), Type: hostagent.RequestDomainApply,
		RequestedBy: by.String(), RequestedAt: time.Now().UTC(),
	})
}
