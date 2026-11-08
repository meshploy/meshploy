package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// The host agent's folder on the cluster agent's machine, as the API reaches
// it: the agent reads and writes it for the API, which writes requests, the
// edge it wants served and the DNS providers' tokens, and reads back what the
// host agent reports. Under IntentPrefix:
//
//	GET host/files/<name>  a file, its modification time in HostModifiedHeader
//	PUT host/files/<name>  a file left whole, root-only with HostPrivateHeader
//	GET host/list/<name>   the files in a folder, as a JSON list of names
const (
	HostFilesPath      = "host/files/"
	HostListPath       = "host/list/"
	HostModifiedHeader = "X-Meshploy-Modified"
	HostPrivateHeader  = "X-Meshploy-Private"
)

// hostCallTimeout bounds one call for the host folder: each is a small file.
const hostCallTimeout = 15 * time.Second

// HostFolder is the host agent's folder on the machine the current agent runs
// on. Without an agent every call fails with ErrNoClusterAgent.
func (c *AgentCluster) HostFolder() hostagent.Folder {
	return agentFolder{func() (*AgentDriver, error) {
		c.mu.RLock()
		defer c.mu.RUnlock()
		if c.cur == nil {
			return nil, ErrNoClusterAgent
		}
		return c.cur.driver, nil
	}}
}

// HostFolder is the host agent's folder on this agent's machine.
func (d *AgentDriver) HostFolder() hostagent.Folder {
	return agentFolder{func() (*AgentDriver, error) { return d, nil }}
}

type agentFolder struct{ driver func() (*AgentDriver, error) }

func (f agentFolder) Read(name string, limit int64) ([]byte, time.Time, error) {
	d, err := f.driver()
	if err != nil {
		return nil, time.Time{}, err
	}
	return d.ReadHostFile(name, limit)
}

func (f agentFolder) List(name string) ([]string, error) {
	d, err := f.driver()
	if err != nil {
		return nil, err
	}
	return d.ListHostFolder(name)
}

func (f agentFolder) Put(name string, data []byte, private bool) error {
	d, err := f.driver()
	if err != nil {
		return err
	}
	return d.PutHostFile(name, data, private)
}

// hostPath is name under prefix, each step escaped.
func hostPath(prefix, name string) (string, error) {
	name, err := hostagent.CleanName(name)
	if err != nil {
		return "", err
	}
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return prefix + strings.Join(parts, "/"), nil
}

func (d *AgentDriver) hostCall(method, path string, body []byte, header http.Header, limit int64) ([]byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	var in io.Reader
	if body != nil {
		in = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.cfg.Host+IntentPrefix+path, in)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, nil, &AgentRefusal{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	if int64(len(raw)) > limit {
		return nil, nil, hostagent.ErrNotAFile
	}
	return raw, resp.Header, nil
}

// ReadHostFile reads a file from the host agent's folder on the agent's
// machine.
func (d *AgentDriver) ReadHostFile(name string, limit int64) ([]byte, time.Time, error) {
	path, err := hostPath(HostFilesPath, name)
	if err != nil {
		return nil, time.Time{}, err
	}
	raw, h, err := d.hostCall(http.MethodGet, path, nil, nil, limit)
	if r, ok := err.(*AgentRefusal); ok && r.Status == http.StatusNotFound {
		return nil, time.Time{}, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, h.Get(HostModifiedHeader))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("the cluster agent did not say when %s changed", name)
	}
	return raw, at.UTC(), nil
}

// ListHostFolder names the files in a folder of the host agent's.
func (d *AgentDriver) ListHostFolder(name string) ([]string, error) {
	path, err := hostPath(HostListPath, name)
	if err != nil {
		return nil, err
	}
	raw, _, err := d.hostCall(http.MethodGet, path, nil, nil, 1<<20)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("the cluster agent's list of %s: %w", name, err)
	}
	return names, nil
}

// PutHostFile leaves a file in the host agent's inbox on the agent's machine.
func (d *AgentDriver) PutHostFile(name string, data []byte, private bool) error {
	path, err := hostPath(HostFilesPath, name)
	if err != nil {
		return err
	}
	h := http.Header{"Content-Type": {"application/octet-stream"}}
	if private {
		h.Set(HostPrivateHeader, "true")
	}
	_, _, err = d.hostCall(http.MethodPut, path, data, h, 64<<10)
	return err
}

// JoinToken asks the current agent for a token node joins the cluster with.
func (c *AgentCluster) JoinToken(ctx context.Context, node string) (*JoinToken, error) {
	a, err := c.conn()
	if err != nil {
		return nil, err
	}
	return a.driver.JoinToken(ctx, node)
}
