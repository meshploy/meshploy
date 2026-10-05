package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Extension point: per-org resource quotas.
//
// CE ships no quotas - quotaCheckers stays empty because the CE binary never
// imports the EE module. EE's MSP mode registers a checker so one install can
// host many orgs with enforced limits, which is also the foundation Meshploy
// Cloud reuses.

// QuotaKind names the resource being created. Values are stable strings rather
// than an enum so an extension can meter kinds CE does not yet know about.
type QuotaKind string

const (
	QuotaProject QuotaKind = "project"
	QuotaService QuotaKind = "service"
	QuotaNode    QuotaKind = "node"
)

// QuotaChecker decides whether an org may create one more of a resource.
// Returning a non-nil error blocks the create and surfaces to the caller, so
// the error text should be user-facing (e.g. "project limit reached (10)").
type QuotaChecker interface {
	CheckQuota(ctx context.Context, orgID uuid.UUID, kind QuotaKind) error
}

var quotaCheckers []QuotaChecker

// RegisterQuotaChecker adds a quota checker. Call from an extension's init():
//
//	func init() { service.RegisterQuotaChecker(orgQuotas{}) }
func RegisterQuotaChecker(qc QuotaChecker) {
	quotaCheckers = append(quotaCheckers, qc)
}

// QuotaCheckersRegistered reports how many quota checkers are registered. Zero
// in a Community build.
func QuotaCheckersRegistered() int { return len(quotaCheckers) }

// checkQuota runs every registered checker, failing on the first rejection.
// A no-op in CE builds, where nothing is registered.
func checkQuota(ctx context.Context, orgID uuid.UUID, kind QuotaKind) error {
	for _, qc := range quotaCheckers {
		if err := qc.CheckQuota(ctx, orgID, kind); err != nil {
			return err
		}
	}
	return nil
}

// Extension point: a service's deletion.

// ServiceDeleteHook runs inside a service's deletion, in its transaction and
// before its row goes, for an extension to remove what it made for that
// service. An error aborts the deletion and is returned to the caller.
type ServiceDeleteHook func(ctx context.Context, tx *gorm.DB, svc *db.Service) error

var serviceDeleteHooks []ServiceDeleteHook

// RegisterServiceDeleteHook adds a hook every service deletion runs. Call from
// an extension's init(). CE registers none.
func RegisterServiceDeleteHook(h ServiceDeleteHook) {
	serviceDeleteHooks = append(serviceDeleteHooks, h)
}

// Extension point: grants derived from something an edition manages.
//
// An edition writes such grants into resource_permissions itself, one row per
// person, with Source "<kind>:<id>", so every check, the mesh policy and the
// edge read them as they read any grant. The console's direct grant and revoke
// touch only rows with no source; a derived row is changed by its source.

// GrantSource names the sources of one kind, for the console to say where a
// derived grant comes from and where it is managed.
type GrantSource struct {
	// Label is the kind as a person reads it.
	Label string
	// Name is one source's name, or "" when it is gone.
	Name func(ctx context.Context, id uuid.UUID) string
	// Link is the console path where the source is managed.
	Link func(id uuid.UUID) string
}

var grantSources = map[string]GrantSource{}

// RegisterGrantSource names the sources of a kind. Call from an extension's
// init(). CE registers none, and writes no derived grants.
func RegisterGrantSource(kind string, src GrantSource) {
	grantSources[kind] = src
}

// GrantVia is where a derived grant comes from, as the console shows it.
type GrantVia struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Name  string `json:"name"`
	Link  string `json:"link,omitempty"`
}

// grantVia describes a grant's source, nil for a direct grant.
func grantVia(ctx context.Context, source string) *GrantVia {
	if source == "" {
		return nil
	}
	kind, rawID, _ := strings.Cut(source, ":")
	v := &GrantVia{Kind: kind, ID: rawID, Label: kind}
	src, ok := grantSources[kind]
	if !ok {
		return v
	}
	v.Label = src.Label
	if id, err := uuid.Parse(rawID); err == nil {
		if src.Name != nil {
			v.Name = src.Name(ctx, id)
		}
		if src.Link != nil {
			v.Link = src.Link(id)
		}
	}
	return v
}

// Extension point: something grants name going away.

// ResourceForgetHook runs when a resource's grants are removed because the
// resource is being deleted, in its transaction, for an extension to remove
// what it keeps about that resource. An error aborts the deletion.
type ResourceForgetHook func(ctx context.Context, tx *gorm.DB, resourceType db.ResourceType, resourceID uuid.UUID) error

// MemberForgetHook runs when a person's grants in an organisation are removed
// because they are leaving it (a member removed, an agent deleted), in its
// transaction. An error aborts the removal.
type MemberForgetHook func(ctx context.Context, tx *gorm.DB, orgID, userID uuid.UUID) error

var (
	resourceForgetHooks []ResourceForgetHook
	memberForgetHooks   []MemberForgetHook
)

// RegisterResourceForgetHook adds a hook every resource deletion runs. Call
// from an extension's init(). CE registers none.
func RegisterResourceForgetHook(h ResourceForgetHook) {
	resourceForgetHooks = append(resourceForgetHooks, h)
}

// RegisterMemberForgetHook adds a hook every removal from an organisation
// runs. Call from an extension's init(). CE registers none.
func RegisterMemberForgetHook(h MemberForgetHook) {
	memberForgetHooks = append(memberForgetHooks, h)
}

// forgetResourceGrants removes every grant on a resource being deleted,
// whatever its source, and lets extensions forget it too.
func forgetResourceGrants(ctx context.Context, tx *gorm.DB, resourceType db.ResourceType, resourceID uuid.UUID) error {
	for _, h := range resourceForgetHooks {
		if err := h(ctx, tx, resourceType, resourceID); err != nil {
			return err
		}
	}
	return tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).Delete(&db.ResourcePermission{}).Error
}

// forgetMemberGrants removes every grant a person holds in an organisation
// they are leaving, whatever its source, and lets extensions forget them too.
func forgetMemberGrants(ctx context.Context, tx *gorm.DB, orgID, userID uuid.UUID) error {
	for _, h := range memberForgetHooks {
		if err := h(ctx, tx, orgID, userID); err != nil {
			return err
		}
	}
	return tx.Where("organization_id = ? AND user_id = ?", orgID, userID).Delete(&db.ResourcePermission{}).Error
}
