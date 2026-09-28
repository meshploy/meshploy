package dokploy

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Taking the edge first (CutoverDeps.EdgeFirst).
//
// The usual cutover waits until every group has moved, because it stops the
// old edge and the domains it still served would go dark. Taking the edge
// first stops nothing: the old edge moves to a side port, and Meshploy's
// Caddy - holding 80 and 443 and terminating TLS for everything - passes each
// domain whose group has not moved to it, through the gateway's proxy, with
// the Host and the X-Forwarded-Proto it arrived with. Those domains are served
// exactly as before; a new Meshploy service gets its domain at once; and each
// group then moves on its own schedule by publishing its routes, which the
// proxy prefers to the fallback.

// defaultSidePort is where the search for a free side port starts.
const defaultSidePort = 18080

// notMovedDomains are the domains the old edge goes on serving: every one
// Dokploy serves that the plan does not rule out - one waiting on a question
// included, since the point is that nothing goes dark - less those of groups
// that have moved.
func notMovedDomains(plan Plan, j *journal.Journal) []string {
	moved := map[string]bool{}
	for _, g := range plan.Groups {
		if groupHasMoved(j, g) {
			for _, h := range GroupDomains(plan, g) {
				moved[h] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, it := range plan.Items {
		if it.Kind != "domain" || it.Verdict == NotMoved {
			continue
		}
		if h := splitHostPath(it.Name); h != "" && !moved[h] && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// sidePort is the port the old edge moves to: the one an earlier attempt
// chose, else the configured one, else the first free one from 18080.
func (d CutoverDeps) sidePort() (int, error) {
	for _, e := range mustEntries(d.Journal) {
		if e.Step == "cutover/fallback" && e.Undo != nil {
			if n, err := strconv.Atoi(e.Undo.Args["side"]); err == nil && n > 0 {
				return n, nil
			}
		}
	}
	if d.SidePort > 0 {
		return d.SidePort, nil
	}
	for p := defaultSidePort; p < defaultSidePort+100; p++ {
		if l, err := net.Listen("tcp", ":"+strconv.Itoa(p)); err == nil {
			_ = l.Close()
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port from %d for the old edge to move to", defaultSidePort)
}

// trustedForwarders are the addresses the old edge may believe an
// X-Forwarded-Proto from: this machine, Docker's bridges, the mesh. Nothing
// else reaches its side port to send one.
var trustedForwarders = []string{"127.0.0.1/32", "172.16.0.0/12", "10.0.0.0/8", "100.64.0.0/10"}

// trustForwardedHeaders makes the old edge's HTTP entrypoint believe the
// X-Forwarded-Proto Meshploy's edge sends, so a request Caddy decrypted is
// served rather than redirected to HTTPS again. The change reaches Traefik
// when the move to the side port restarts it; a rollback writes the file back.
func (d CutoverDeps) trustForwardedHeaders() error {
	step := "cutover/trust-forwarded"
	if d.Journal.Done(step) {
		return nil
	}
	b, err := os.ReadFile(d.TraefikConfig)
	if err != nil {
		return fmt.Errorf("read the old edge's configuration: %w", err)
	}
	content := string(b)
	const web = "  web:\n    address: :80\n"
	if !strings.Contains(content, web) {
		return fmt.Errorf("%s has no web entrypoint on :80 in the shape Dokploy writes; set its forwardedHeaders.trustedIPs by hand, then cut over", d.TraefikConfig)
	}
	if strings.Contains(content, web+"    forwardedHeaders:") {
		return d.Journal.Append(journal.Entry{Step: step, Action: "trust-forwarded", Target: d.TraefikConfig, Result: journal.OK})
	}
	var trust strings.Builder
	trust.WriteString(web + "    forwardedHeaders:\n      trustedIPs:\n")
	for _, cidr := range trustedForwarders {
		trust.WriteString("        - " + cidr + "\n")
	}
	backup := filepath.Join(d.Journal.Dir(), "traefik.yml.before-edge-first")
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(d.TraefikConfig, []byte(strings.Replace(content, web, trust.String(), 1))); err != nil {
		return err
	}
	return d.Journal.Append(journal.Entry{Step: step, Action: "trust-forwarded", Target: d.TraefikConfig, Result: journal.OK,
		Undo: &journal.Undo{Kind: journal.UndoRestoreFile, Args: map[string]string{"path": d.TraefikConfig, "backup": backup}}})
}

// setFallback tells the proxy to send the old edge's domains to its side port,
// and waits until it does.
func (d CutoverDeps) setFallback(side int, hosts []string) error {
	step := "cutover/fallback"
	upstream := "127.0.0.1:" + strconv.Itoa(side)
	if !d.Journal.Done(step) {
		if err := d.Fallback.SetEdgeFallback(upstream, hosts); err != nil {
			return fmt.Errorf("tell the proxy where the old edge will be: %w", err)
		}
		if err := d.Journal.Append(journal.Entry{Step: step, Action: "set-edge-fallback", Target: upstream, Result: journal.OK,
			Undo: &journal.Undo{Kind: journal.UndoClearEdgeFallback, Args: map[string]string{"side": strconv.Itoa(side)}}}); err != nil {
			return err
		}
	}
	if len(hosts) == 0 || d.ProxyAddr == "" {
		return nil
	}
	return d.waitProxyFallback(hosts[0])
}

// waitProxyFallback asks the proxy for one of the old edge's domains until it
// stops answering with its own "no route" page: it reads the fallback on its
// next refresh.
func (d CutoverDeps) waitProxyFallback(host string) error {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(90 * time.Second)
	for {
		req, _ := http.NewRequest(http.MethodGet, "http://"+d.ProxyAddr+"/", nil)
		req.Host = host
		if resp, err := client.Do(req); err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "No route") {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the gateway's proxy has not picked up where %s goes after 90 s", host)
		}
		time.Sleep(2 * time.Second)
	}
}

// moveEdgeAside takes the old edge off 80 and 443 and puts its HTTP entrypoint
// on the side port, recorded under the step a failed cutover restores.
//
// Docker removes a published port by its target, protocol and mode, never by
// the published number, and a bare "80" means ingress mode: Dokploy's Traefik
// publishes in host mode, so `--publish-rm 80` removed nothing, the edge kept
// 80 beside its side port, and it crash-looped once Caddy took 80. On Docker
// 29 that churn tore down the overlay every Dokploy app is reached through.
// So the removal names each port's own mode, and the move is not recorded
// until the edge runs with the side port alone.
func (d CutoverDeps) moveEdgeAside(side int) error {
	step := "cutover/stop-edge"
	if d.Journal.Done(step) {
		return nil
	}
	modes, err := d.edgePortModes()
	if err != nil {
		return fmt.Errorf("read how %s publishes its ports: %w", d.Edge.Name, err)
	}
	sideStr := strconv.Itoa(side)
	if _, err := d.Runner.Output("docker", "service", "update", "--detach",
		"--publish-rm", "mode="+modes[80]+",target=80", "--publish-rm", "mode="+modes[443]+",target=443",
		"--publish-add", "mode=host,published="+sideStr+",target=80",
		d.Edge.Name); err != nil {
		return fmt.Errorf("move %s to port %d: %w", d.Edge.Name, side, err)
	}
	undo := &journal.Undo{Kind: journal.UndoRepublishService, Args: map[string]string{"service": d.Edge.Name, "side": sideStr,
		"mode80": modes[80], "mode443": modes[443]}}
	if err := d.awaitAside(side); err != nil {
		// Recorded as failed with its undo, so the cutover's own rollback
		// gives the edge its ports back.
		_ = d.Journal.Append(journal.Entry{Step: step, Action: "move-edge-aside", Target: d.Edge.Name + " → :" + sideStr,
			Result: journal.Failed, Error: err.Error(), Undo: undo})
		return err
	}
	return d.Journal.Append(journal.Entry{Step: step, Action: "move-edge-aside", Target: d.Edge.Name + " → :" + sideStr, Result: journal.OK,
		Undo: undo})
}

// edgePortModes is the publish mode of the edge's 80 and 443, host unless the
// service says otherwise.
func (d CutoverDeps) edgePortModes() (map[int]string, error) {
	out, err := d.Runner.Output("docker", "service", "inspect", d.Edge.Name, "--format", "{{json .Spec.EndpointSpec.Ports}}")
	if err != nil {
		return nil, err
	}
	modes := map[int]string{80: "host", 443: "host"}
	var ports []struct {
		TargetPort  int
		PublishMode string
	}
	if strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "null" {
		if err := json.Unmarshal([]byte(out), &ports); err != nil {
			return nil, err
		}
	}
	for _, p := range ports {
		if (p.TargetPort == 80 || p.TargetPort == 443) && p.PublishMode != "" {
			modes[p.TargetPort] = p.PublishMode
		}
	}
	return modes, nil
}

// awaitAside waits for the edge to publish the side port and nothing else, and
// for a task of it to be running: what Caddy is about to take must be free,
// and what the fallback sends to must answer.
func (d CutoverDeps) awaitAside(side int) error {
	window := d.EdgeGoneWindow
	if window == 0 {
		window = edgeGoneWindow
	}
	deadline := time.Now().Add(window)
	want := fmt.Sprintf("%d->80", side)
	last := ""
	for {
		ports, _ := d.Runner.Output("docker", "service", "inspect", d.Edge.Name, "--format",
			"{{range .Endpoint.Ports}}{{.PublishedPort}}->{{.TargetPort}} {{end}}")
		state, _ := d.Runner.Output("docker", "service", "ps", d.Edge.Name, "--filter", "desired-state=running", "--format", "{{.CurrentState}}")
		last = strings.TrimSpace(ports)
		if last == want && strings.HasPrefix(strings.TrimSpace(state), "Running") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not settle on port %d alone (publishes %q, task %q)", d.Edge.Name, side, last, strings.TrimSpace(state))
		}
		time.Sleep(time.Second)
	}
}

func mustEntries(j *journal.Journal) []journal.Entry {
	if j == nil {
		return nil
	}
	entries, _ := journal.Read(j.Dir())
	return entries
}
