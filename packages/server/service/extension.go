package service

import (
	"context"

	"github.com/google/uuid"
)

// Extension point: per-org resource quotas.
//
// CE ships no quotas — quotaCheckers stays empty because the CE binary never
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
