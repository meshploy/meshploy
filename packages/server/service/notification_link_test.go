package service

import (
	"testing"

	meshdb "github.com/meshploy/packages/db"
)

// Without a domain service - or with no domain rows - a notification still
// links somewhere: the install's FRONTEND_URL.
func TestNotificationLinksFallBackToFrontendURL(t *testing.T) {
	s := &NotificationService{consoleURL: "https://console.old.test/"}
	n := s.notice(meshdb.DefaultBrand, "Ops", "deploy.failed", NotificationData{Link: "/projects/p"})
	if n.Link != "https://console.old.test/projects/p" {
		t.Fatalf("link = %q", n.Link)
	}
}
