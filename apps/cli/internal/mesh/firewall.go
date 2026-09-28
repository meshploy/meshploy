package mesh

import "strings"

// Keeping the mesh's firewall hook first.
//
// A stock Tailscale on the same machine, left in its default netfilter mode,
// puts its own INPUT hook at the top every time it starts, and that hook
// drops every packet from the CGNAT range that did not arrive on tailscale0.
// Meshploy's mesh uses that range too, on meshploy0, so for as long as the
// stock hook is first the mesh is cut off.
//
// Meshploy's daemon hooks mp-input, which accepts traffic on meshploy0 and
// returns everything else untouched: first in line, it takes only its own and
// the other tailnet sees no difference. KeepFirst puts it back in front
// whenever something moved it; the host agent runs it every few seconds, so a
// restart of the other Tailscale costs the mesh about a second.

// Runner runs a command and returns its output.
type Runner func(name string, args ...string) (string, error)

// KeepFirst moves mp-input back in front of the stock Tailscale's ts-input, for
// IPv4 and IPv6, where the mesh daemon has created it and ts-input got ahead,
// or puts the hook back where it went missing. It reports what it moved.
//
// Only ts-input is overtaken. Other hooks ahead of the mesh's - kube-router's
// network-policy chain, which k3s re-inserts at the top every sync - drop
// nothing of the mesh's, and moving ahead of them turned into a tug of war
// that also stepped in front of the cluster's own policy checks.
func KeepFirst(run Runner) []string {
	var moved []string
	for _, tool := range []string{"iptables", "ip6tables"} {
		if _, err := run(tool, "-S", "mp-input"); err != nil {
			continue // no mesh chain here: the daemon is not running, or not in this family
		}
		out, err := run(tool, "-S", "INPUT")
		if err != nil {
			continue
		}
		mesh, stock := -1, -1
		n := 0
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "-A INPUT") {
				continue
			}
			switch {
			case line == "-A INPUT -j mp-input" && mesh < 0:
				mesh = n
			case line == "-A INPUT -j ts-input" && stock < 0:
				stock = n
			}
			n++
		}
		if mesh >= 0 && (stock < 0 || mesh < stock) {
			continue
		}
		// Delete every copy, then insert one at the top, so a hook that was
		// never there and one that slipped down end the same.
		for i := 0; i < 5; i++ {
			if _, err := run(tool, "-D", "INPUT", "-j", "mp-input"); err != nil {
				break
			}
		}
		if _, err := run(tool, "-I", "INPUT", "1", "-j", "mp-input"); err == nil {
			if mesh < 0 {
				moved = append(moved, tool+": mp-input put back in INPUT")
			} else {
				moved = append(moved, tool+": mp-input moved ahead of ts-input")
			}
		}
	}
	return moved
}
