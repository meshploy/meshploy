package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The ticket exists to make a credential that appears in a URL — and therefore
// in request logs — worthless to whoever reads that log later. Both properties
// that deliver it are pinned here: one redemption only, and a short life.

func TestTicketRedeemsOnceAndReturnsTheMintingUser(t *testing.T) {
	s := &TicketService{tickets: map[string]wsTicket{}}
	userID := uuid.New()

	tok, exp, err := s.Mint(userID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if tok == "" {
		t.Fatal("mint returned an empty ticket")
	}
	if !exp.After(time.Now()) {
		t.Fatal("a freshly minted ticket must not already be expired")
	}

	got, ok := s.Redeem(tok)
	if !ok {
		t.Fatal("the first redemption must succeed")
	}
	if got != userID {
		t.Fatalf("redeem returned the wrong user: got %s, want %s", got, userID)
	}

	// The load-bearing property: a ticket recovered from a log cannot be reused.
	if _, ok := s.Redeem(tok); ok {
		t.Fatal("a ticket must not be redeemable twice")
	}
}

func TestExpiredTicketIsRejected(t *testing.T) {
	s := &TicketService{tickets: map[string]wsTicket{}}
	userID := uuid.New()

	// Plant an already-expired ticket rather than sleeping out TicketTTL.
	s.tickets["stale"] = wsTicket{userID: userID, expires: time.Now().Add(-time.Second)}

	if _, ok := s.Redeem("stale"); ok {
		t.Fatal("an expired ticket must be rejected")
	}
	// It must also be gone, so a slow clock cannot make it valid again.
	if _, present := s.tickets["stale"]; present {
		t.Fatal("redeeming an expired ticket must still consume it")
	}
}

func TestUnknownAndEmptyTicketsAreRejected(t *testing.T) {
	s := &TicketService{tickets: map[string]wsTicket{}}

	if _, ok := s.Redeem(""); ok {
		t.Fatal("an empty ticket must be rejected")
	}
	if _, ok := s.Redeem("not-a-real-ticket"); ok {
		t.Fatal("an unknown ticket must be rejected")
	}
}

func TestTicketsAreUnique(t *testing.T) {
	s := &TicketService{tickets: map[string]wsTicket{}}
	userID := uuid.New()

	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, _, err := s.Mint(userID)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		if seen[tok] {
			t.Fatal("Mint produced a duplicate ticket")
		}
		seen[tok] = true
	}
}

// TicketTTL is a security parameter: it bounds how long a ticket captured from
// a log stays usable. A large value would quietly undo the point of the change.
func TestTicketTTLStaysShort(t *testing.T) {
	if TicketTTL > time.Minute {
		t.Fatalf("TicketTTL is %v — a ticket travels in the URL and lands in request logs, so it must stay short", TicketTTL)
	}
}
