package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TicketTTL bounds how long a minted ticket stays redeemable.
//
// The window only has to cover "the browser received the ticket and opened the
// WebSocket", which is one round trip. Keeping it short is the point: a ticket
// travels in the URL query string, and anything in a URL ends up in request
// logs. A logged ticket that expired seconds ago is worthless, whereas the
// session JWT this replaces stayed valid for its full lifetime.
const TicketTTL = 30 * time.Second

type wsTicket struct {
	userID  uuid.UUID
	expires time.Time
}

// TicketService mints single-use, short-lived tickets that authenticate a
// WebSocket upgrade.
//
// Browsers cannot set headers on a WebSocket handshake, so the credential has
// to ride in the URL. Sending the session JWT there put a full-privilege bearer
// token into every log that records request URIs — including this API's own
// chi request logger, whose output is the container log on disk. A ticket is
// the smallest thing that closes that: it proves identity once, within seconds,
// and is useless afterwards.
//
// Tickets are held in memory rather than a table. They live for seconds, and
// losing them on restart costs a reconnect rather than an outage.
type TicketService struct {
	mu      sync.Mutex
	tickets map[string]wsTicket
}

func NewTicketService() *TicketService {
	s := &TicketService{tickets: map[string]wsTicket{}}
	go s.reap()
	return s
}

// Mint issues a ticket for userID. The caller must already be authenticated by
// the normal header-based middleware — a ticket carries no more authority than
// the identity it names, and every terminal handler still runs its own
// org/resource authorization after redeeming one.
func (s *TicketService) Mint(userID uuid.UUID) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	tok := hex.EncodeToString(raw)
	exp := time.Now().Add(TicketTTL)

	s.mu.Lock()
	s.tickets[tok] = wsTicket{userID: userID, expires: exp}
	s.mu.Unlock()

	return tok, exp, nil
}

// Redeem consumes a ticket and returns the user it identifies.
//
// The delete happens whether or not the ticket had expired, so a leaked ticket
// cannot be retried: one presentation is all it ever gets.
func (s *TicketService) Redeem(tok string) (uuid.UUID, bool) {
	if tok == "" {
		return uuid.Nil, false
	}

	s.mu.Lock()
	t, ok := s.tickets[tok]
	delete(s.tickets, tok)
	s.mu.Unlock()

	if !ok || time.Now().After(t.expires) {
		return uuid.Nil, false
	}
	return t.userID, true
}

// reap drops expired tickets so an unredeemed one cannot accumulate. Redeeming
// already removes them; this only cleans up tickets nobody ever used.
func (s *TicketService) reap() {
	for range time.Tick(time.Minute) {
		now := time.Now()
		s.mu.Lock()
		for tok, t := range s.tickets {
			if now.After(t.expires) {
				delete(s.tickets, tok)
			}
		}
		s.mu.Unlock()
	}
}
