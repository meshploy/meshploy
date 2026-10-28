package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrConflict: a name Meshploy would write already answers somewhere else,
// through a record that is not Meshploy's. It is the operator's, so it is
// reported rather than replaced.
var ErrConflict = errors.New("a record that is not Meshploy's is in the way")

// ZoneFor returns the zone that holds name: the longest of zones whose name is
// name or a parent of it. A delegated subzone is its own zone, so the longest
// match is the one whose records the name is actually served from.
func ZoneFor(zones []Zone, name string) (Zone, bool) {
	var best Zone
	found := false
	for _, z := range zones {
		if (name == z.Name || strings.HasSuffix(name, "."+z.Name)) && len(z.Name) > len(best.Name) {
			best, found = z, true
		}
	}
	return best, found
}

// pointingType is the record type that points a name at ip.
func pointingType(ip string) string {
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		return "AAAA"
	}
	return "A"
}

// pointing reports whether r sends its name somewhere, as an A, AAAA or CNAME
// record does; a TXT or MX record beside it is no concern of Meshploy's.
func pointing(r Record) bool {
	switch r.Type {
	case "A", "AAAA", "CNAME":
		return true
	}
	return false
}

// wildcardKeeper is a provider whose wildcards go on answering for names
// below a record that exists under them. By RFC 4592 a wildcard stops there,
// so on a provider that follows it, the challenge records Caddy writes under
// a domain would cut that domain's names off from a wildcard above it.
type wildcardKeeper interface {
	WildcardsAnswerBelowRecords() bool
}

// parentAnswers reports whether a wildcard above domain in the same zone
// already points it at ip, on a provider where that is enough by itself.
func parentAnswers(ctx context.Context, p Provider, zone Zone, domain, ip string) (bool, error) {
	if k, ok := p.(wildcardKeeper); !ok || !k.WildcardsAnswerBelowRecords() {
		return false, nil
	}
	typ := pointingType(ip)
	for name := domain; strings.Contains(name, "."); {
		_, name, _ = strings.Cut(name, ".")
		if name != zone.Name && !strings.HasSuffix(name, "."+zone.Name) {
			break
		}
		recs, err := p.Records(ctx, zone, "*."+name)
		if err != nil {
			return false, err
		}
		for _, r := range recs {
			if pointing(r) {
				// The closest wildcard decides, whatever it points at.
				return r.Type == typ && r.Content == ip, nil
			}
		}
	}
	return false, nil
}

// PointDomain writes the records that send a base domain to the gateway, each
// marked with comment, and leaves the operator's own alone:
//
//   - nothing, where a wildcard above the domain already points it at ip and
//     the provider keeps it answering through challenges (wildcardKeeper):
//     *.eu.meshploy.cloud carries every space under it, with no record each.
//   - otherwise *.<domain>, and <domain> itself only when it has no records
//     at all: one that has some is something the operator already serves.
//
// A record of the operator's already pointing the wildcard at ip is used as
// it is. One pointing it anywhere else is ErrConflict.
func PointDomain(ctx context.Context, p Provider, zone Zone, domain, ip, comment string) error {
	if ok, err := parentAnswers(ctx, p, zone, domain, ip); err != nil || ok {
		return err
	}
	typ := pointingType(ip)
	wildcard := "*." + domain
	recs, err := p.Records(ctx, zone, wildcard)
	if err != nil {
		return err
	}
	answered := false
	for _, r := range recs {
		if !pointing(r) {
			continue
		}
		if r.Comment == comment {
			if r.Type == typ && r.Content == ip {
				answered = true
				continue
			}
			// Ours, and stale: the gateway's address changed.
			if err := p.DeleteRecord(ctx, zone, r.ID); err != nil {
				return err
			}
			continue
		}
		if r.Type == typ && r.Content == ip {
			answered = true
			continue
		}
		return fmt.Errorf("%w: %s has a %s record for %s; remove it, or point it at %s", ErrConflict, wildcard, r.Type, r.Content, ip)
	}
	if !answered {
		if _, err := p.CreateRecord(ctx, zone, Record{Type: typ, Name: wildcard, Content: ip, Comment: comment}); err != nil {
			return err
		}
	}

	apex, err := p.Records(ctx, zone, domain)
	if err != nil {
		return err
	}
	if len(apex) == 0 {
		if _, err := p.CreateRecord(ctx, zone, Record{Type: typ, Name: domain, Content: ip, Comment: comment}); err != nil {
			return err
		}
	}
	return nil
}

// Pointed reports whether the domain's names answer with ip, through its own
// wildcard or one above it that PointDomain relies on, read through the
// provider rather than from DNS, so a cache cannot stand in for the zone.
func Pointed(ctx context.Context, p Provider, zone Zone, domain, ip string) (bool, error) {
	if ok, err := parentAnswers(ctx, p, zone, domain, ip); err != nil || ok {
		return ok, err
	}
	recs, err := p.Records(ctx, zone, "*."+domain)
	if err != nil {
		return false, err
	}
	typ := pointingType(ip)
	for _, r := range recs {
		if r.Type == typ && r.Content == ip {
			return true, nil
		}
	}
	return false, nil
}

// UnpointDomain removes the records PointDomain wrote, found by their comment.
// Nothing without it is touched, so an operator's record under the same name
// stays.
func UnpointDomain(ctx context.Context, p Provider, zone Zone, domain, comment string) error {
	for _, name := range []string{"*." + domain, domain} {
		recs, err := p.Records(ctx, zone, name)
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.Comment != comment {
				continue
			}
			if err := p.DeleteRecord(ctx, zone, r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
