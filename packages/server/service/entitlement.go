package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/license"
	"github.com/meshploy/packages/server/config"
	"gorm.io/gorm"
)

// LicensePublicKeys are the Ed25519 public keys this build trusts, base64.
//
// Overridden at build time:
//
//	-ldflags "-X github.com/meshploy/packages/server/service.LicensePublicKeys=<key>"
//
// Multiple keys are comma-separated, current first. Accepting a list is what
// makes key rotation survivable: ship a release trusting both the new and the
// previous key, reissue, then drop the old one. A single key would mean a
// compromise invalidates every customer's license at once.
//
// Empty by default, so a stock CE build trusts nothing and no license can
// activate — the safe default. Generate one with `meshploy-admin license keygen`.
var LicensePublicKeys = ""

// Status is the entitlement snapshot the UI renders.
type Status struct {
	Licensed  bool      `json:"licensed"`
	Tier      string    `json:"tier,omitempty"`
	Customer  string    `json:"customer,omitempty"`
	Features  []string  `json:"features"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	Expired   bool      `json:"expired"`
	NodeLimit int       `json:"node_limit,omitempty"` // 0 = unlimited
	NodeCount int       `json:"node_count"`
	OverLimit bool      `json:"over_limit"`
	Problem   string    `json:"problem,omitempty"` // why an installed license is not active

	// RegistryScope names the private image this license grants access to, so a
	// client can act on it without the operator having to know the repository
	// path. Not a secret: it is a claim inside the customer's own token, and
	// knowing the name grants nothing without registry credentials.
	RegistryScope string `json:"registry_scope,omitempty"`

	// CanActivate reports whether this build trusts any signing key. False in a
	// stock CE build, where activation always fails.
	//
	// The UI needs this to explain the upgrade path instead of surfacing a bare
	// "this build trusts no license signing key" after someone pastes a licence
	// they just paid for. The order is switch image, then activate — a CE binary
	// cannot store a licence at all.
	CanActivate bool `json:"can_activate"`
}

// Entitlements answers "may this org use feature X".
//
// One interface, two sources: self-hosted verifies an offline `mlic-` token
// (install-scoped, so orgID is ignored), while Cloud will read org.plan
// (org-scoped). Feature code calls Entitled(ctx, orgID, "sso") and never knows
// which is behind it — that is what makes Cloud cheap to add later.
type Entitlements interface {
	Entitled(ctx context.Context, orgID uuid.UUID, feature string) bool
	Describe(ctx context.Context, orgID uuid.UUID) (Status, error)
}

// EntitlementService is the self-hosted implementation.
type EntitlementService struct {
	db     *gorm.DB
	domain string // the install's base domain, for domain-bound licenses

	mu     sync.RWMutex
	loaded bool
	claims license.License
	active bool   // signature valid, not expired, domain matches
	reason string // why not active
}

var _ Entitlements = (*EntitlementService)(nil)

// trustedKeys parses the configured public keys, skipping malformed entries so
// one bad key cannot disable licensing entirely.
func trustedKeys() []string {
	var out []string
	for _, k := range strings.Split(LicensePublicKeys, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// load reads and verifies the stored token, caching the result. Verification is
// cheap but pointless per-request, and the token only changes when an admin
// pastes a new one — which calls Invalidate.
func (s *EntitlementService) load(ctx context.Context) {
	s.mu.RLock()
	done := s.loaded
	s.mu.RUnlock()
	if done {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.loaded = true
	s.active, s.reason, s.claims = false, "", license.License{}

	var row db.InstalledLicense
	if err := s.db.WithContext(ctx).First(&row).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			s.reason = "could not read the installed license"
		}
		return
	}

	keys := trustedKeys()
	if len(keys) == 0 {
		s.reason = "this build trusts no license signing key"
		return
	}

	var lastErr error
	for _, encoded := range keys {
		pub, err := license.DecodePublicKey(encoded)
		if err != nil {
			lastErr = err
			continue
		}
		claims, err := license.Verify(pub, row.Token)
		switch {
		case err == nil:
			s.claims = claims
			if !claims.MatchesDomain(s.domain) {
				s.reason = "license is not valid for this domain"
				return
			}
			s.active = true
			return
		case errors.Is(err, license.ErrExpired):
			// Signature is good — degrade, don't brick. Keep the claims so the
			// UI can show renewal details, but grant nothing.
			s.claims = claims
			s.reason = "license expired"
			return
		default:
			lastErr = err
		}
	}
	if lastErr != nil {
		s.reason = "license signature is not valid for this build"
	}
}

// Invalidate drops the cache. Call after storing a new token.
func (s *EntitlementService) Invalidate() {
	s.mu.Lock()
	s.loaded = false
	s.mu.Unlock()
}

// Entitled reports whether the named feature is unlocked. orgID is ignored for
// self-hosted licenses, which are install-scoped.
func (s *EntitlementService) Entitled(ctx context.Context, _ uuid.UUID, feature string) bool {
	s.load(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active && s.claims.HasFeature(feature)
}

// Describe returns the snapshot the UI renders, including soft node metering.
func (s *EntitlementService) Describe(ctx context.Context, _ uuid.UUID) (Status, error) {
	s.load(ctx)
	s.mu.RLock()
	claims, active, reason := s.claims, s.active, s.reason
	s.mu.RUnlock()

	st := Status{
		Licensed:    active,
		Features:    claims.Features,
		Problem:     reason,
		CanActivate: len(trustedKeys()) > 0,
	}
	if st.Features == nil {
		st.Features = []string{}
	}
	if claims.LicenseID != "" {
		st.Tier = claims.Tier
		st.Customer = claims.Customer
		st.ExpiresAt = claims.ExpiresAt
		st.Expired = claims.Expired()
		st.NodeLimit = claims.NodeLimit
		st.RegistryScope = claims.RegistryScope
	}

	// Soft node metering: worker nodes only. The gateway is fixed overhead, so
	// counting it would make the HA-gateway feature consume the allowance.
	// Over-limit is reported, never enforced — blocking node registration would
	// make the paid edition more restrictive than CE, and would break scaling
	// during an incident over an accounting matter.
	if active && claims.NodeLimit > 0 {
		var workers int64
		if err := s.db.WithContext(ctx).Model(&db.Node{}).
			Where("k3s_role <> ?", db.K3sRoleServer).Count(&workers).Error; err != nil {
			return st, err
		}
		st.NodeCount = int(workers)
		st.OverLimit = int(workers) > claims.NodeLimit
	}
	return st, nil
}

// Activate verifies a token before storing it, so an admin gets immediate
// feedback rather than silently saving something that will never work.
//
// A valid token stored on a CE binary still unlocks nothing — EE features are
// not compiled in. That is the point: the real gate is the private EE image,
// which is why MIT on CE is safe. The message tells the admin exactly that.
func (s *EntitlementService) Activate(ctx context.Context, token string, byUser uuid.UUID) (Status, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Status{}, errors.New("license token is required")
	}
	keys := trustedKeys()
	if len(keys) == 0 {
		return Status{}, errors.New("this build trusts no license signing key")
	}

	var verified bool
	var claims license.License
	for _, encoded := range keys {
		pub, err := license.DecodePublicKey(encoded)
		if err != nil {
			continue
		}
		c, err := license.Verify(pub, token)
		if err == nil || errors.Is(err, license.ErrExpired) {
			claims, verified = c, true
			if errors.Is(err, license.ErrExpired) {
				return Status{}, errors.New("this license has expired")
			}
			break
		}
	}
	if !verified {
		return Status{}, errors.New("license token is not valid")
	}
	if !claims.MatchesDomain(s.domain) {
		return Status{}, errors.New("this license is not valid for this install's domain")
	}

	// Single row per install: replace whatever is there.
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&db.InstalledLicense{}).Error; err != nil {
		return Status{}, err
	}
	if err := s.db.WithContext(ctx).Create(&db.InstalledLicense{
		Token: token, ActivatedBy: byUser,
	}).Error; err != nil {
		return Status{}, err
	}
	s.Invalidate()
	return s.Describe(ctx, uuid.Nil)
}

// entitlementDomain returns the install's base domain for domain-bound
// licenses. Empty when unconfigured, which only matters for a bound license.
func entitlementDomain(c *config.Config) string {
	if c == nil {
		return ""
	}
	return c.Domain
}
