package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatewayCfg is the seeding install.sh writes on a master node. Without at
// least one of these the machine is somebody's laptop, not a gateway.
func gatewayCfg(firewall string) *config.Config {
	return &config.Config{
		PublicIP:          "203.0.113.10",
		GatewayIP:         "100.64.0.1",
		FirewallState:     firewall,
		FirewallCheckedAt: "2026-09-10T01:00:00Z",
	}
}

func TestExposureIgnoresNonGateways(t *testing.T) {
	svc := service.New(newTestDB(t), &config.Config{})
	got, err := svc.System.GetExposure(context.Background(), uuid.New())
	require.NoError(t, err)

	// A developer running the API locally must never be told their machine is
	// exposed -- the same signal the setup gate uses.
	require.Equal(t, "unknown", got.FirewallState)
	require.Empty(t, got.Ports)
}

func TestExposureReportsPortsOnlyWithoutFirewall(t *testing.T) {
	cases := []struct {
		firewall  string
		wantState string
		wantPorts bool
	}{
		{"none", "none", true},
		{"ufw", "ufw", false},
		{"firewalld", "firewalld", false},
		// An install predating the check records nothing. That is unknown, not
		// a problem: reporting it as exposed would cry wolf on every gateway
		// installed before this shipped.
		{"", "unknown", false},
	}
	for _, c := range cases {
		t.Run(c.firewall, func(t *testing.T) {
			svc := service.New(newTestDB(t), gatewayCfg(c.firewall))
			got, err := svc.System.GetExposure(context.Background(), uuid.New())
			require.NoError(t, err)
			require.Equal(t, c.wantState, got.FirewallState)
			if c.wantPorts {
				require.NotEmpty(t, got.Ports)
			} else {
				require.Empty(t, got.Ports)
			}
		})
	}
}

// The registry is the reason this notice exists: it is the one published port
// that has no authentication of its own, so losing the firewall is the only
// thing standing in front of it.
func TestExposureNamesTheUnauthenticatedRegistry(t *testing.T) {
	svc := service.New(newTestDB(t), gatewayCfg("none"))
	got, err := svc.System.GetExposure(context.Background(), uuid.New())
	require.NoError(t, err)

	var found bool
	for _, p := range got.Ports {
		if p.Port == 5000 {
			found = true
			require.NotEmpty(t, p.Note, "the registry port must say why it matters")
		}
	}
	require.True(t, found, "port 5000 must be reported")
	require.Equal(t, "2026-09-10T01:00:00Z", got.CheckedAt,
		"the notice must carry when it was observed, so it never reads as live")
}

func TestDismissNoticeIsPerUserAndIdempotent(t *testing.T) {
	svc := service.New(newTestDB(t), gatewayCfg("none"))
	ctx := context.Background()
	alice, bob := uuid.New(), uuid.New()

	got, err := svc.System.GetExposure(ctx, alice)
	require.NoError(t, err)
	require.False(t, got.Dismissed)

	require.NoError(t, svc.System.DismissNotice(ctx, alice, service.NoticeHostExposure))
	// Twice must not error: a double-click or a retry is not a failure.
	require.NoError(t, svc.System.DismissNotice(ctx, alice, service.NoticeHostExposure))

	got, err = svc.System.GetExposure(ctx, alice)
	require.NoError(t, err)
	require.True(t, got.Dismissed)

	// Bob still sees it. One operator dismissing a gateway advisory must not
	// hide it from the next person who signs in.
	got, err = svc.System.GetExposure(ctx, bob)
	require.NoError(t, err)
	require.False(t, got.Dismissed)
	require.NotEmpty(t, got.Ports)
}

func TestDismissNoticeRejectsUnknownKey(t *testing.T) {
	svc := service.New(newTestDB(t), gatewayCfg("none"))
	err := svc.System.DismissNotice(context.Background(), uuid.New(), "not-a-notice")
	require.Error(t, err)
}

// The console asks once for everything this user has waved away, so a panel
// that hides itself does not need a round trip of its own.
func TestDismissedNoticesListsWhatThisUserDismissed(t *testing.T) {
	svc := service.New(newTestDB(t), gatewayCfg("none"))
	ctx := context.Background()
	alice, bob := uuid.New(), uuid.New()

	keys, err := svc.System.DismissedNotices(ctx, alice)
	require.NoError(t, err)
	require.Empty(t, keys)

	require.NoError(t, svc.System.DismissNotice(ctx, alice, service.NoticeGettingStarted))
	require.NoError(t, svc.System.DismissNotice(ctx, alice, service.NoticeHostExposure))

	keys, err = svc.System.DismissedNotices(ctx, alice)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{service.NoticeGettingStarted, service.NoticeHostExposure}, keys)

	keys, err = svc.System.DismissedNotices(ctx, bob)
	require.NoError(t, err)
	require.Empty(t, keys, "one user's dismissal is not another's")
}

// The console cannot see the host, so it can only say what install.sh
// recorded. The DNS mode decides whether an internal route gets a real
// certificate or one from Caddy's own CA, which is a difference worth showing
// rather than promising around.
func TestExposureCarriesTheDNSMode(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()

	t.Run("a gateway that manages its own DNS", func(t *testing.T) {
		cfg := gatewayCfg("ufw")
		cfg.DNSMode = "ondemand"
		got, err := service.New(newTestDB(t), cfg).System.GetExposure(ctx, user)
		require.NoError(t, err)
		assert.Equal(t, "ondemand", got.DNSMode)
	})

	t.Run("a delegated one", func(t *testing.T) {
		cfg := gatewayCfg("ufw")
		cfg.DNSMode = "delegation"
		got, err := service.New(newTestDB(t), cfg).System.GetExposure(ctx, user)
		require.NoError(t, err)
		assert.Equal(t, "delegation", got.DNSMode)
	})

	// A machine that is not a gateway returns early, and still has to answer:
	// the console asks the same question everywhere.
	t.Run("a machine that is not a gateway", func(t *testing.T) {
		cfg := &config.Config{DNSMode: "ondemand"}
		got, err := service.New(newTestDB(t), cfg).System.GetExposure(ctx, user)
		require.NoError(t, err)
		assert.Equal(t, "ondemand", got.DNSMode)
	})
}
