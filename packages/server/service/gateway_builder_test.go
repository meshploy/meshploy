package service_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const gatewayIP = "100.64.0.1"

// seedOrg creates an organisation and, when role is not nil, a gateway node for
// it holding that build role, as an install from before this default would.
func seedOrg(t *testing.T, db *gorm.DB, slug string, role *meshdb.MeshRole) uuid.UUID {
	t.Helper()
	org := meshdb.Organization{Name: slug, Slug: slug}
	require.NoError(t, db.Create(&org).Error)
	if role != nil {
		require.NoError(t, db.Create(&meshdb.Node{
			OrganizationID: org.ID, Name: "srv1854405", TailscaleIP: gatewayIP,
			K3sRole: meshdb.K3sRoleServer, MeshRole: *role,
		}).Error)
	}
	return org.ID
}

func gatewayRole(t *testing.T, db *gorm.DB, orgID uuid.UUID) meshdb.MeshRole {
	t.Helper()
	var n meshdb.Node
	if err := db.Where("organization_id = ? AND tailscale_ip = ?", orgID, gatewayIP).First(&n).Error; err != nil {
		return "(none)"
	}
	return n.MeshRole
}

// startSeeding builds the services the way a gateway's API starts, which seeds
// every existing organisation a moment later.
func startSeeding(db *gorm.DB) {
	service.New(db, &config.Config{GatewayHostname: "srv1854405", GatewayIP: gatewayIP, Domain: "gateway.example.test"})
}

// A single-server install has to be able to build: its gateway is the only node.
func TestNewGatewayIsABuildNode(t *testing.T) {
	db := newTestDB(t)
	org := seedOrg(t, db, "fresh", nil)
	startSeeding(db)
	require.Eventually(t, func() bool { return gatewayRole(t, db, org) == meshdb.MeshRoleWorkloadBuilder },
		10*time.Second, 100*time.Millisecond, "the seeded gateway should build by default")
}

// A gateway installed before the default existed has an empty role, meaning
// nobody chose; it takes the default too.
func TestExistingGatewayWithNoRoleBecomesABuildNode(t *testing.T) {
	db := newTestDB(t)
	unset := meshdb.MeshRole("")
	org := seedOrg(t, db, "older", &unset)
	startSeeding(db)
	require.Eventually(t, func() bool { return gatewayRole(t, db, org) == meshdb.MeshRoleWorkloadBuilder },
		10*time.Second, 100*time.Millisecond)
}

// Turning "Act as build node" off stores workload, and that choice survives
// every restart of the API.
func TestGatewayOptOutIsKept(t *testing.T) {
	db := newTestDB(t)
	optedOut := meshdb.MeshRoleWorkload
	org := seedOrg(t, db, "optedout", &optedOut)
	startSeeding(db)

	// The domain is seeded after the build role is defaulted, so once it exists
	// the default has had its chance.
	require.Eventually(t, func() bool {
		var n int64
		db.Table("domains").Where("organization_id = ?", org).Count(&n)
		return n > 0
	}, 10*time.Second, 100*time.Millisecond, "seeding never reached this organisation")
	require.Equal(t, meshdb.MeshRoleWorkload, gatewayRole(t, db, org), "the opt-out was overridden")
}
