package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NoticeHostExposure keys the console banner that reports a gateway running
// without a host firewall. A stable slug, not a message: the copy can be
// rewritten without silently un-dismissing it for everyone.
const NoticeHostExposure = "host-exposure"

// ExposedPort is one port Meshploy itself publishes on all interfaces.
type ExposedPort struct {
	Port    int    `json:"port"`
	Service string `json:"service"`
	// Note names why this particular port matters, where that is not obvious
	// from the service name alone.
	Note string `json:"note,omitempty"`
}

// Exposure reports whether this gateway was installed onto a host with no
// firewall, and what Meshploy publishes if so.
//
// It is an advisory, not a verdict. The ports are what Meshploy binds, taken
// from its own compose file and k3s -- not the result of probing, which the API
// container could not do anyway. Whether they are actually reachable depends on
// the operator's network: a cloud security group, a NAT gateway or an upstream
// appliance is invisible from the host, and any of them may already contain
// this. That is why the notice is dismissible rather than a nag.
type Exposure struct {
	// FirewallState is what install.sh saw on the host: none, ufw, firewalld,
	// or unknown when it was never recorded (a dev box, or an install predating
	// the check).
	FirewallState string `json:"firewall_state"`
	// CheckedAt is when the installer looked, RFC3339. A point-in-time
	// observation -- the console shows it so the notice never reads as live.
	CheckedAt string `json:"checked_at,omitempty"`
	// Ports is populated only when FirewallState is "none"; empty otherwise.
	Ports []ExposedPort `json:"ports"`
	// Dismissed reports whether the current user has already dismissed this.
	Dismissed bool `json:"dismissed"`
}

// publishedPorts are the ports Meshploy binds on all interfaces on a gateway.
//
// Kept as a literal rather than parsed from docker-compose.yml because the API
// container does not mount it -- and because two of these are k3s's, not
// compose's. If a port here stops being published, this list is what has to
// change with it.
func publishedPorts() []ExposedPort {
	return []ExposedPort{
		{Port: 5000, Service: "container registry", Note: "accepts anonymous push, pull and delete"},
		{Port: 4000, Service: "meshploy api", Note: "authenticated, but plain HTTP"},
		{Port: 6443, Service: "kubernetes api"},
		{Port: 10250, Service: "kubelet"},
		{Port: 9100, Service: "node metrics"},
	}
}

// GetExposure reports the host-exposure advisory for one user.
//
// Returns FirewallState "unknown" with no ports on anything that is not a
// provisioned gateway, so a developer running the API locally is never told
// their laptop is exposed. The signal is the same one the setup gate uses:
// install.sh seeds a public IP, mesh IP or hostname, and a machine with none of
// them was never installed by it.
func (s *SystemService) GetExposure(ctx context.Context, userID uuid.UUID) (Exposure, error) {
	out := Exposure{FirewallState: "unknown", Ports: []ExposedPort{}}
	if s.cfg == nil {
		return out, nil
	}
	if s.cfg.PublicIP == "" && s.cfg.GatewayIP == "" && s.cfg.GatewayHostname == "" {
		return out, nil
	}

	if s.cfg.FirewallState != "" {
		out.FirewallState = s.cfg.FirewallState
	}
	out.CheckedAt = s.cfg.FirewallCheckedAt
	if out.FirewallState == "none" {
		out.Ports = publishedPorts()
	}

	dismissed, err := s.noticeDismissed(ctx, userID, NoticeHostExposure)
	if err != nil {
		return out, err
	}
	out.Dismissed = dismissed
	return out, nil
}

// DismissNotice marks one advisory as dismissed for one user. Idempotent, so a
// double-click or a retry does not error.
func (s *SystemService) DismissNotice(ctx context.Context, userID uuid.UUID, key string) error {
	if s.db == nil {
		return errors.New("no database")
	}
	if key != NoticeHostExposure {
		return errors.New("unknown notice")
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&meshdb.DismissedNotice{UserID: userID, Key: key}).Error
}

func (s *SystemService) noticeDismissed(ctx context.Context, userID uuid.UUID, key string) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var row meshdb.DismissedNotice
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND key = ?", userID, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
