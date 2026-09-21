package service

import (
	"strings"
	"testing"
)

// Every event the code dispatches must be in the catalogue, or it cannot be
// subscribed to and is never delivered. job.failed was in exactly that state
// for months, dispatched and unsubscribable.
func TestEveryDispatchedEventIsInTheCatalogue(t *testing.T) {
	// The events dispatched across the service package, by hand so that adding
	// a Dispatch call without a catalogue entry fails here.
	dispatched := []string{
		"deploy.success", "deploy.failed",
		"service.crashed", "service.recovered",
		"job.success", "job.failed",
		"backup.success", "backup.failed", "backup.skipped",
		"restore.success", "restore.failed",
		"node.offline", "node.online",
		"member.joined", "agent.token_created",
	}
	for _, event := range dispatched {
		if _, ok := eventDef(event); !ok {
			t.Errorf("%s is dispatched but not in the catalogue, so nothing can subscribe to it", event)
		}
	}
	if len(eventCatalogue) != len(dispatched) {
		t.Errorf("catalogue has %d events, %d are dispatched: one side has drifted", len(eventCatalogue), len(dispatched))
	}
}

// A catalogue entry with no tone renders a grey chip and a colourless message.
func TestEveryEventHasATitleToneAndDescription(t *testing.T) {
	for _, d := range eventCatalogue {
		if d.Title == "" || d.Description == "" || d.Group == "" {
			t.Errorf("%s is missing a title, description or group", d.Event)
		}
		if _, ok := toneHex[d.Tone]; !ok {
			t.Errorf("%s has tone %q, which has no colour", d.Event, d.Tone)
		}
		if !strings.Contains(d.Event, ".") {
			t.Errorf("%s does not follow <resource>.<outcome>", d.Event)
		}
	}
}

// The default selection is what a channel starts with, so it has to be the set
// that means something needs attention. Not only failures: backups that are
// not running because a database is stopped is silence, and silence is the one
// thing nobody notices on their own.
func TestRecommendedIsWhatNeedsAttention(t *testing.T) {
	want := map[string]bool{
		"deploy.failed": true, "service.crashed": true, "job.failed": true,
		"backup.failed": true, "backup.skipped": true, "restore.failed": true,
		"node.offline": true, "member.joined": true, "agent.token_created": true,
	}
	for _, d := range eventCatalogue {
		if d.Recommended != want[d.Event] {
			t.Errorf("%s: recommended=%v, want %v", d.Event, d.Recommended, want[d.Event])
		}
	}
}

// A service event says which stack it belongs to, or that it belongs to none.
func TestOriginNamesTheStack(t *testing.T) {
	if got := (NotificationData{ServiceName: "redis", StackName: "infra"}).origin(); got != "stack infra" {
		t.Errorf("got %q, want %q", got, "stack infra")
	}
	if got := (NotificationData{ServiceName: "redis"}).origin(); got != "standalone" {
		t.Errorf("got %q, want %q", got, "standalone")
	}
	if got := (NotificationData{NodeName: "gateway"}).origin(); got != "" {
		t.Errorf("a node event should not claim an origin, got %q", got)
	}
}
