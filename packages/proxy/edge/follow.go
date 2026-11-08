package edge

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Parts are what a snapshot is served by.
type Parts struct {
	Serve   func(*Snapshot)
	Restore func()
	// Close, when set, runs as Run returns: what the edge opened is closed.
	Close func()
}

// Follower keeps an edge serving the API's snapshot: once at start, again
// within a moment of any change (the API holds the request until its versions
// move), and in full every Full regardless, which is how brand and stopped
// services, which move no version, reach the edge.
type Follower struct {
	Client *Client
	Parts  Parts
	// Full is how often the whole snapshot is asked for even when nothing
	// moved. Zero is 30 seconds.
	Full time.Duration
	// Wait is how long the API is asked to hold a request. Zero is MaxWait.
	Wait time.Duration
	// Retry is the first pause after a failure, doubled up to a minute.
	// Zero is a second.
	Retry time.Duration
}

// Run follows until ctx ends. A first fetch that fails serves the saved copy
// (Parts.Restore) until one succeeds; a later failure keeps what is served.
func (f *Follower) Run(ctx context.Context) {
	full, wait, retry := f.Full, f.Wait, f.Retry
	if full <= 0 {
		full = 30 * time.Second
	}
	if wait <= 0 || wait > MaxWait {
		wait = MaxWait
	}
	if retry <= 0 {
		retry = time.Second
	}
	if f.Parts.Close != nil {
		defer f.Parts.Close()
	}
	var have *Versions
	var lastFull time.Time
	pause := retry
	restored := false
	for ctx.Err() == nil {
		ask := have
		if time.Since(lastFull) >= full {
			ask = nil
		}
		s, err := f.Client.Fetch(ctx, ask, wait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("edge: the API did not give the snapshot, keeping what is served: %v", err)
			if have == nil && !restored {
				f.Parts.Restore()
				restored = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
			if pause *= 2; pause > time.Minute {
				pause = time.Minute
			}
			continue
		}
		pause = retry
		if s == nil {
			continue // nothing moved
		}
		f.Parts.Serve(s)
		v := s.Versions
		have = &v
		if ask == nil {
			lastFull = time.Now()
		}
	}
}

// PagesHolder answers the edge's own pages from the last snapshot served.
type PagesHolder struct{ p atomic.Pointer[Pages] }

// Set has the pages answered from p.
func (h *PagesHolder) Set(p *Pages) { h.p.Store(p) }

func (h *PagesHolder) BrandName(ctx context.Context, host string, orgID uuid.UUID) string {
	return h.p.Load().BrandName(ctx, host, orgID)
}

func (h *PagesHolder) Stopped(ctx context.Context, serviceID uuid.UUID) bool {
	return h.p.Load().Stopped(ctx, serviceID)
}
