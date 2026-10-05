package service

import (
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ErrTooManyAttempts is a sign-in step tried too often for one account: the
// password for one address, or the second factor for one person. Limited per
// account as well as per address of origin, since guesses spread across many
// machines get past a limit by origin alone.
var ErrTooManyAttempts = errors.New("too many attempts: wait a minute and try again")

// attempts allows a few tries at once per key, then one a minute.
type attempts struct {
	mu    sync.Mutex
	every time.Duration
	burst int
	m     map[string]*attempt
}

type attempt struct {
	lim  *rate.Limiter
	seen time.Time
}

func newAttempts(every time.Duration, burst int) *attempts {
	return &attempts{every: every, burst: burst, m: map[string]*attempt{}}
}

// Allow reports whether one more try for key is allowed now.
func (a *attempts) Allow(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	// Forget keys idle long enough to be back at a full allowance.
	if len(a.m) > 10000 {
		for k, v := range a.m {
			if now.Sub(v.seen) > a.every*time.Duration(a.burst) {
				delete(a.m, k)
			}
		}
	}
	e, ok := a.m[key]
	if !ok {
		e = &attempt{lim: rate.NewLimiter(rate.Every(a.every), a.burst)}
		a.m[key] = e
	}
	e.seen = now
	return e.lim.Allow()
}

var (
	// passwordAttempts: ten tries for one sign-in address, then one a minute.
	passwordAttempts = newAttempts(time.Minute, 10)
	// codeAttempts: five tries at a person's second factor (an authenticator
	// code or a recovery code), then one a minute. A six-digit code is a
	// million guesses; five a minute make it out of reach.
	codeAttempts = newAttempts(time.Minute, 5)
)
