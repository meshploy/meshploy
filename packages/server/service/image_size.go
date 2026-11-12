package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// imageDownloadSize is how many bytes a node downloads to pull ref for its
// os and arch: the compressed layers and the config, as its registry lists
// them. It asks anonymously, as a node pulling a public image does; a private
// image's size is not known, and that is an error.
func imageDownloadSize(ctx context.Context, ref, os, arch string) (int64, error) {
	key := ref + "|" + os + "|" + arch
	if v, ok := imageSizes.Load(key); ok {
		if c := v.(cachedSize); time.Since(c.at) < imageSizeKept {
			return c.size, nil
		}
	}
	host, repo, tag := splitImageRef(ref)
	size, err := registrySize(ctx, registryHTTP, "https://"+host, repo, tag, os, arch)
	if err != nil {
		return 0, err
	}
	imageSizes.Store(key, cachedSize{size: size, at: time.Now()})
	return size, nil
}

type cachedSize struct {
	size int64
	at   time.Time
}

// A tag can move to a new build, so a size is asked again after a while.
const imageSizeKept = time.Hour

var (
	imageSizes   sync.Map
	registryHTTP = &http.Client{Timeout: 10 * time.Second}
)

// splitImageRef splits an image reference into its registry, repository and
// tag or digest, with Docker Hub's defaults: no registry is Docker Hub, an
// official image is under library/, and no tag is latest.
func splitImageRef(ref string) (host, repo, tag string) {
	host = "registry-1.docker.io"
	rest := ref
	if i := strings.IndexByte(ref, '/'); i > 0 {
		first := ref[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			host, rest = first, ref[i+1:]
		}
	}
	if host == "docker.io" || host == "index.docker.io" {
		host = "registry-1.docker.io"
	}
	if i := strings.Index(rest, "@"); i >= 0 {
		rest, tag = rest[:i], rest[i+1:]
	} else if i := strings.LastIndexByte(rest, ':'); i > strings.LastIndexByte(rest, '/') {
		rest, tag = rest[:i], rest[i+1:]
	}
	if tag == "" {
		tag = "latest"
	}
	if host == "registry-1.docker.io" && !strings.Contains(rest, "/") {
		rest = "library/" + rest
	}
	return host, rest, tag
}

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

type pullManifest struct {
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	Config struct {
		Size int64 `json:"size"`
	} `json:"config"`
	Layers []struct {
		Size int64 `json:"size"`
	} `json:"layers"`
}

// registrySize reads ref's manifest from the registry at base, following an
// index to the os and arch's own manifest.
func registrySize(ctx context.Context, c *http.Client, base, repo, ref, os, arch string) (int64, error) {
	token := ""
	m, err := fetchManifest(ctx, c, base, repo, ref, &token)
	if err != nil {
		return 0, err
	}
	if len(m.Manifests) > 0 {
		digest := ""
		for _, e := range m.Manifests {
			if e.Platform.OS == os && e.Platform.Architecture == arch {
				digest = e.Digest
				break
			}
		}
		if digest == "" {
			return 0, fmt.Errorf("no %s/%s image under %s", os, arch, ref)
		}
		if m, err = fetchManifest(ctx, c, base, repo, digest, &token); err != nil {
			return 0, err
		}
	}
	if len(m.Layers) == 0 {
		return 0, errors.New("the manifest lists no layers")
	}
	total := m.Config.Size
	for _, l := range m.Layers {
		total += l.Size
	}
	return total, nil
}

// fetchManifest gets one manifest, taking an anonymous token the first time
// the registry asks for one and keeping it for the next request.
func fetchManifest(ctx context.Context, c *http.Client, base, repo, ref string, token *string) (*pullManifest, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, ref), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", manifestAccept)
		if *token != "" {
			req.Header.Set("Authorization", "Bearer "+*token)
		}
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if *token, err = anonymousToken(ctx, c, resp.Header.Get("WWW-Authenticate")); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("registry answered %d for %s:%s", resp.StatusCode, repo, ref)
		}
		var m pullManifest
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
}

// anonymousToken asks a registry's token service for a pull token without
// credentials, as its WWW-Authenticate challenge says to.
func anonymousToken(ctx context.Context, c *http.Client, challenge string) (string, error) {
	scheme, params, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", errors.New("the registry wants credentials")
	}
	p := map[string]string{}
	for _, part := range strings.Split(params, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			p[k] = strings.Trim(v, `"`)
		}
	}
	realm, err := url.Parse(p["realm"])
	if err != nil || realm.Scheme == "" {
		return "", errors.New("the registry named no token service")
	}
	q := realm.Query()
	for _, k := range []string{"service", "scope"} {
		if p[k] != "" {
			q.Set(k, p[k])
		}
	}
	realm.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the token service answered %d", resp.StatusCode)
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return "", err
	}
	if t.Token == "" {
		t.Token = t.AccessToken
	}
	if t.Token == "" {
		return "", errors.New("the token service gave no token")
	}
	return t.Token, nil
}
