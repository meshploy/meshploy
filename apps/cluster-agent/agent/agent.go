// Package agent is the cluster agent's server: it holds the cluster's
// credential on the machine and answers the Meshploy API over the mesh, each
// Kubernetes request checked by the policy before it is sent on with the
// agent's own credential, and the pods that reach the host made here, on
// request, from Meshploy's own templates.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/meshploy/apps/cluster-agent/policy"
	"github.com/meshploy/packages/hostagent"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// maxBody caps a request body; the largest the API sends is a Secret.
const maxBody = 16 << 20

// Agent is the handler the API's requests reach once they are known to come
// from it.
type Agent struct {
	cs       kubernetes.Interface
	host     *url.URL
	proxy    *httputil.ReverseProxy
	direct   *http.Client // the agent's own calls: dry runs, current objects
	facts    *ClusterFacts
	builds   BuildOptions
	hostDir  hostagent.Folder // nil refuses requests for the host agent
	logf     func(format string, args ...any)
	timeNow  func() time.Time
	dryLimit time.Duration
}

// BuildOptions is how the agent makes builds.
type BuildOptions struct {
	// Image is the builder the agent runs, whatever the API names. Empty
	// refuses builds.
	Image string
}

// New is an agent for the cluster cfg reaches, with its admin credential.
func New(cfg *rest.Config, facts *ClusterFacts, builds BuildOptions) (*Agent, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return newAgent(cs, cfg, facts, builds)
}

// newAgent is New with the clientset given, so a test can hand it a fake one
// while requests still go to cfg's address.
func newAgent(cs kubernetes.Interface, cfg *rest.Config, facts *ClusterFacts, builds BuildOptions) (*Agent, error) {
	host, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := rest.TLSConfigFor(cfg)
	if err != nil {
		return nil, err
	}
	direct, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}
	return &Agent{
		cs: cs, host: host, proxy: newProxy(host, tlsCfg, cfg), direct: direct,
		facts: facts, builds: builds, logf: log.Printf, timeNow: time.Now, dryLimit: 20 * time.Second,
	}, nil
}

// newProxy forwards to the cluster's API server over HTTP/1.1, so exec and
// port-forward can upgrade, with the agent's credential in place of anything
// the caller sent.
func newProxy(target *url.URL, tlsCfg *tls.Config, cfg *rest.Config) *httputil.ReverseProxy {
	if tlsCfg == nil {
		tlsCfg = &tls.Config{}
	}
	tlsCfg = tlsCfg.Clone()
	tlsCfg.NextProtos = []string{"http/1.1"}
	tr := &http.Transport{
		TLSClientConfig: tlsCfg, ForceAttemptHTTP2: false,
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	p := httputil.NewSingleHostReverseProxy(target)
	p.Transport = tr
	p.FlushInterval = -1 // watches and logs stream
	orig := p.Director
	p.Director = func(r *http.Request) {
		orig(r)
		stripCallerIdentity(r.Header)
		if cfg.BearerToken != "" {
			r.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
		}
		r.Host = target.Host
	}
	return p
}

// stripCallerIdentity removes every header through which a caller could
// speak as someone: the API's own token, and impersonation, which the
// agent's admin credential would otherwise honour.
func stripCallerIdentity(h http.Header) {
	h.Del("Authorization")
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "impersonate-") {
			h.Del(k)
		}
	}
}

func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, IntentPrefix) {
		a.serveIntent(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		a.refuse(w, r, "a body larger than the agent takes")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))

	req := policy.Request{
		Method:  r.Method,
		Path:    r.URL.Path,
		Upgrade: isUpgrade(r),
		Now:     a.timeNow(),
	}
	d := policy.Decide(req, a.facts)
	if d.NeedObject {
		stored, answer, err := a.dryRun(r, body)
		if err != nil {
			a.logf("FAIL  %s %s: dry run: %v", r.Method, r.URL.Path, err)
			http.Error(w, "the cluster agent could not check this write: "+err.Error(), http.StatusBadGateway)
			return
		}
		if answer != nil {
			// The API server refuses the write itself: the caller gets its
			// answer, as it would have for the write.
			a.logf("API   %s %s: refused by the API server (%d)", r.Method, r.URL.Path, answer.status)
			answer.write(w)
			return
		}
		req.Stored = stored
		if r.Method == http.MethodPut || r.Method == http.MethodPatch {
			current, err := a.current(r.Context(), r.URL.Path)
			if err != nil {
				a.logf("FAIL  %s %s: current object: %v", r.Method, r.URL.Path, err)
				http.Error(w, "the cluster agent could not read the object being changed: "+err.Error(), http.StatusBadGateway)
				return
			}
			req.Current = current
		}
		if d = policy.Decide(req, a.facts); d.NeedObject {
			d = policy.Decision{Reason: "no answer from its stored form"}
		}
	}
	if !d.Allow {
		a.refuse(w, r, d.Reason)
		return
	}
	a.logf("ALLOW %s %s: %s", r.Method, r.URL.Path, d.Reason)
	a.proxy.ServeHTTP(w, r)
}

func isUpgrade(r *http.Request) bool {
	for _, v := range r.Header.Values("Connection") {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), "upgrade") {
				return true
			}
		}
	}
	return r.Header.Get("Upgrade") != ""
}

// refuse answers as the API server does, with a Status the client libraries
// read as Forbidden.
func (a *Agent) refuse(w http.ResponseWriter, r *http.Request, reason string) {
	a.logf("DENY  %s %s: %s", r.Method, r.URL.Path, reason)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,"message":%q}`,
		"the cluster agent refused this: "+reason)
}

type apiAnswer struct {
	status      int
	contentType string
	body        []byte
}

func (a *apiAnswer) write(w http.ResponseWriter) {
	if a.contentType != "" {
		w.Header().Set("Content-Type", a.contentType)
	}
	w.WriteHeader(a.status)
	w.Write(a.body)
}

// dryRun sends a write to the API server as a server-side dry run with the
// agent's credential, and returns the object the server would store. When
// the server refuses the write, its answer is returned instead.
func (a *Agent) dryRun(r *http.Request, body []byte) ([]byte, *apiAnswer, error) {
	u := *a.host
	u.Path = r.URL.Path
	q := r.URL.Query()
	q.Del("dryRun")
	q.Set("dryRun", "All")
	u.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(r.Context(), a.dryLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.direct.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &apiAnswer{resp.StatusCode, resp.Header.Get("Content-Type"), out}, nil
	}
	return out, nil, nil
}

// current is the object at path as the API server holds it now.
func (a *Agent) current(ctx context.Context, path string) ([]byte, error) {
	u := *a.host
	u.Path = path
	ctx, cancel := context.WithTimeout(ctx, a.dryLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.direct.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return out, nil
}
