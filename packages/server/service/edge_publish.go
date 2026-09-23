package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
		PublicIP:  s.cfg.PublicIP,
		MeshIP:    s.cfg.GatewayIP,
		WrittenAt: time.Now().UTC(),
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
		snap.Domains = append(snap.Domains, hostagent.EdgeDomain{
			BaseDomain:        d.BaseDomain,
			InternalSubdomain: internal,
			DNSMode:           mode,
			Primary:           d.IsPrimary,
			ServesPlatform:    d.FormerPrimary,
		})
	}
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

// queueEdgeApply leaves a domain.apply request in the inbox.
func (s *SystemService) queueEdgeApply(by uuid.UUID) error {
	req := hostagent.Request{
		ID: uuid.NewString(), Type: hostagent.RequestDomainApply,
		RequestedBy: by.String(), RequestedAt: time.Now().UTC(),
	}
	if err := hostagent.ValidateRequest(req, ""); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	inbox := hostagent.InboxDir(s.hostDir())
	// Written under a temporary name and renamed, so the agent never reads half
	// a request.
	tmp, err := os.CreateTemp(inbox, ".request-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), inbox+"/"+hostagent.RequestDomainApply+"-"+req.ID+".json")
}
