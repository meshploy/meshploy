package service

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type DomainService struct {
	db *gorm.DB
	// git is consulted before a domain is removed: providers may still call it.
	git *GitIntegrationService
	// installDomain is DOMAIN from the install, used only when an organisation
	// has no primary domain row - a gateway seeded before domains were rows,
	// or a developer's machine.
	installDomain string
}

// BackfillDNSModes gives a DNS mode to domains that predate the column.
//
// Only rows with none: every row written since carries its own, and a mode
// switched in the console must never be overwritten by DNS_MODE, which records
// only what the installer was told. Idempotent - once filled there is nothing
// empty left to fill.
func (s *DomainService) BackfillDNSModes(ctx context.Context, installMode string) error {
	mode := db.DNSModeDelegation
	if installMode == string(db.DNSModeOnDemand) {
		mode = db.DNSModeOnDemand
	}
	return s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("dns_mode = ''").Update("dns_mode", mode).Error
}

// Primary returns the organisation's primary domain, or nil when it has none.
func (s *DomainService) Primary(ctx context.Context, orgID uuid.UUID) (*db.Domain, error) {
	var d db.Domain
	err := s.db.WithContext(ctx).
		Where("organization_id = ? AND is_primary", orgID).First(&d).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// PlatformURL is where one of the platform's own services is reached from the
// internet: https://<sub>.<primary domain>. Empty when there is no domain at
// all to build it from.
//
// Read from the primary rather than from DOMAIN, because the primary can move.
// And never HEADSCALE_URL: that is the API's own address for Headscale inside
// the compose network (http://headscale:8080), which nothing outside the
// gateway can resolve. Handing it to a machine being provisioned is how a
// one-command join failed at the first step.
func (s *DomainService) PlatformURL(ctx context.Context, orgID uuid.UUID, sub string) string {
	if d, err := s.Primary(ctx, orgID); err == nil && d != nil {
		return "https://" + sub + "." + d.BaseDomain
	}
	if s.installDomain != "" {
		return "https://" + sub + "." + s.installDomain
	}
	return ""
}

// GatewayPlatformURL is PlatformURL for a caller that has no organisation - an
// unauthenticated script download. A Community gateway serves one
// organisation, so its primary is the gateway's.
func (s *DomainService) GatewayPlatformURL(ctx context.Context, sub string) string {
	var d db.Domain
	if err := s.db.WithContext(ctx).Where("is_primary").Order("created_at ASC").First(&d).Error; err == nil {
		return "https://" + sub + "." + d.BaseDomain
	}
	if s.installDomain != "" {
		return "https://" + sub + "." + s.installDomain
	}
	return ""
}

// hostnameRe is one label, dot, at least one more label. Deliberately stricter
// than RFC 1123: a base domain has to have a dot in it, because a single label
// cannot be delegated and cannot hold a public certificate.
var hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

func (s *DomainService) List(ctx context.Context, orgID uuid.UUID) ([]db.Domain, error) {
	domains := make([]db.Domain, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).
		// Primary first, then oldest: the order the picker wants, so the console
		// does not re-sort what the API already knows.
		Order("is_primary DESC, created_at ASC").Find(&domains).Error
	return domains, err
}

func (s *DomainService) Get(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	var domain db.Domain
	err := s.db.WithContext(ctx).First(&domain, "id = ?", domainID).Error
	return &domain, err
}

// Create adds a base domain, unverified and not primary. Ownership is proved
// afterwards by a TXT record, the same way a custom domain on a route is.
//
// It is never primary on creation: promoting one moves the platform's own
// hostnames, which is a separate decision taken when the domain is already
// serving, not while it is still a name somebody typed.
func (s *DomainService) Create(ctx context.Context, orgID uuid.UUID, baseDomain string, mode db.DNSMode) (*db.Domain, error) {
	name := strings.ToLower(strings.TrimSpace(baseDomain))
	name = strings.TrimSuffix(name, ".")
	if !hostnameRe.MatchString(name) {
		return nil, huma.Error422UnprocessableEntity(
			fmt.Sprintf("%q is not a domain name - give a name with a dot in it, no scheme, port or wildcard", baseDomain))
	}
	switch mode {
	case "":
		mode = db.DNSModeDelegation
	case db.DNSModeDelegation, db.DNSModeOnDemand:
	default:
		return nil, huma.Error422UnprocessableEntity(
			fmt.Sprintf("unknown DNS mode %q", mode))
	}

	token, err := generateVerifyToken()
	if err != nil {
		return nil, fmt.Errorf("generate verify token: %w", err)
	}
	domain := &db.Domain{
		OrganizationID:    orgID,
		BaseDomain:        name,
		InternalSubdomain: "internal",
		PreviewSubdomain:  "preview",
		DNSMode:           mode,
		Verified:          false,
		VerifyToken:       token,
	}
	if err := s.db.WithContext(ctx).Create(domain).Error; err != nil {
		// base_domain is unique across every org, which is what stops one org
		// claiming another's. The claimant cannot be named without leaking who
		// else is on this gateway, so it is not.
		if isUniqueViolation(err) {
			return nil, huma.Error409Conflict(fmt.Sprintf("%s is already registered", name))
		}
		return nil, err
	}
	return domain, nil
}

// Verify looks for the ownership TXT record and marks the domain verified.
// Idempotent: a verified domain answers success without another lookup, because
// a re-verify should not be able to un-verify anything.
func (s *DomainService) Verify(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if domain.Verified {
		return domain, nil
	}
	records, err := net.LookupTXT("_meshploy-verify." + domain.BaseDomain)
	if err != nil || !containsToken(records, domain.VerifyToken) {
		return nil, huma.Error422UnprocessableEntity("TXT record not found or not yet propagated")
	}
	domain.Verified = true
	return domain, s.db.WithContext(ctx).Model(domain).Update("verified", true).Error
}

// SetDNSMode changes how a domain's DNS is arranged. Per domain: two base
// domains on one gateway can differ, and the edge config is rendered from these
// rows rather than chosen once for the server.
//
// The rows are the source of truth; nothing at the edge changes until the
// config is regenerated and applied.
func (s *DomainService) SetDNSMode(ctx context.Context, domainID uuid.UUID, mode db.DNSMode) (*db.Domain, error) {
	if mode != db.DNSModeDelegation && mode != db.DNSModeOnDemand {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("unknown DNS mode %q", mode))
	}
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	domain.DNSMode = mode
	return domain, s.db.WithContext(ctx).Model(domain).Update("dns_mode", mode).Error
}

// StartRetiring takes a domain out of the running for new routes.
//
// It is a write, and that is the point of it: once the domain leaves the
// picker, nothing new can attach, so the list of what still holds it only ever
// shrinks. A checklist that could grow while somebody worked through it would
// not be one they could finish.
//
// Nothing stops serving. Existing routes answer, certificates renew, the zone
// is served; retiring a domain is deciding to remove it, not removing it.
func (s *DomainService) StartRetiring(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if domain.IsPrimary {
		return nil, huma.Error422UnprocessableEntity(
			"the primary domain cannot be retired - the console, the API and Headscale are served on it. Make another domain primary first")
	}
	if !domain.Verified {
		return nil, huma.Error422UnprocessableEntity(
			"this domain was never verified, so nothing was ever served on it - remove it instead")
	}
	if domain.RetiringAt != nil {
		return domain, nil
	}
	now := time.Now().UTC()
	domain.RetiringAt = &now
	return domain, s.db.WithContext(ctx).Model(domain).Update("retiring_at", now).Error
}

// StopRetiring puts a retiring domain back in the picker. It undoes the
// decision and nothing else: routes moved off it in the meantime stay moved.
func (s *DomainService) StopRetiring(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if domain.RetiringAt == nil {
		return domain, nil
	}
	domain.RetiringAt = nil
	return domain, s.db.WithContext(ctx).Model(domain).Update("retiring_at", nil).Error
}

// Delete removes a base domain. Three refusals, and they are different in
// kind: the primary is the platform's own address and there is nothing to fall
// back to; a domain that has served routes is removed by retiring it first, so
// the removal is a decision somebody saw through rather than a click; and a
// domain with routes is still answering for somebody.
//
// A domain that was never verified skips retiring: nothing was ever served on
// it, so there is nothing to wind down - it is most often a typo.
func (s *DomainService) Delete(ctx context.Context, domainID uuid.UUID) error {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return err
	}
	if domain.IsPrimary {
		return huma.Error422UnprocessableEntity(
			"this is the primary domain - the console, the API and Headscale are served on it. Make another domain primary first")
	}
	if domain.Verified && domain.RetiringAt == nil {
		return huma.Error422UnprocessableEntity(
			"start retiring this domain first - that stops new routes attaching, and shows what still holds it")
	}
	// A former primary is also the name workers' control connections go to.
	onIt, err := s.NodesOnDomain(ctx, domain.OrganizationID, domain)
	if err != nil {
		return err
	}
	if len(onIt) > 0 {
		return errNodesOnDomain(len(onIt), domain.BaseDomain)
	}
	// And the names CI jobs call to deploy.
	hooks, err := s.DeployHooksOnDomain(ctx, domain.OrganizationID, domain)
	if err != nil {
		return err
	}
	if len(hooks) > 0 {
		return errDeployHooksOnDomain(len(hooks), domain.BaseDomain)
	}
	// And the name git providers call.
	if s.git != nil {
		regs, err := s.IntegrationsOnDomain(ctx, s.git, domain.OrganizationID, domain)
		if err != nil {
			return err
		}
		if len(regs) > 0 {
			return errIntegrationsOnDomain(len(regs), domain.BaseDomain)
		}
	}
	var routes int64
	if err := s.db.WithContext(ctx).Model(&db.Route{}).
		Where("domain_id = ?", domainID).Count(&routes).Error; err != nil {
		return err
	}
	if routes > 0 {
		// The FK is RESTRICT, so the database would refuse anyway. Saying how
		// many and what to do beats a constraint violation.
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"%d route(s) still use this domain - move or remove them first", routes))
	}
	return s.db.WithContext(ctx).Delete(&db.Domain{}, "id = ?", domainID).Error
}

// BaseDomainFor returns the verified base domain a hostname sits under, or nil.
//
// Matching is done in Go over the org's domains rather than in SQL: there are a
// handful of them, and the alternative is a suffix comparison the database
// cannot index anyway.
func (s *DomainService) BaseDomainFor(ctx context.Context, hostname string) (*db.Domain, error) {
	host := strings.ToLower(strings.TrimSpace(hostname))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return nil, nil
	}
	domains := make([]db.Domain, 0)
	if err := s.db.WithContext(ctx).Where("verified = ?", true).Find(&domains).Error; err != nil {
		return nil, err
	}
	var best *db.Domain
	for i := range domains {
		if !strings.HasSuffix(host, "."+domains[i].BaseDomain) {
			continue
		}
		// Longest wins, so a domain delegated under another one is not shadowed
		// by its parent.
		if best == nil || len(domains[i].BaseDomain) > len(best.BaseDomain) {
			best = &domains[i]
		}
	}
	return best, nil
}

// CreateSeeded creates the base domain as already verified and primary. Used
// during gateway auto-seeding where DNS ownership is implicit (we control
// CoreDNS). Silently returns nil if a domain already exists for the org.
func (s *DomainService) CreateSeeded(ctx context.Context, orgID uuid.UUID, baseDomain string, mode db.DNSMode) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("organization_id = ?", orgID).Count(&count).Error; err != nil {
		return err
	}
	if count >= 1 {
		return nil // already seeded
	}
	if mode != db.DNSModeOnDemand {
		mode = db.DNSModeDelegation
	}
	domain := &db.Domain{
		OrganizationID:    orgID,
		BaseDomain:        baseDomain,
		InternalSubdomain: "internal",
		PreviewSubdomain:  "preview",
		DNSMode:           mode,
		IsPrimary:         true,
		Verified:          true,
	}
	return s.db.WithContext(ctx).Create(domain).Error
}
