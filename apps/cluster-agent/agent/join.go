package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
)

// A machine joining the cluster needs a token the k3s server accepts. The
// server's own token never expires and adds any machine, so the API is never
// given it: for each join the agent mints a bootstrap token that lasts an
// hour, and only the agent decides how long.

// JoinTokenTTL is how long a join token lasts: long enough for an install,
// short enough that one copied from a log is soon worthless.
const JoinTokenTTL = time.Hour

// joinToken is a k3s bootstrap token in its full form, the server's CA hash
// first so the joining machine checks whom it joins.
var joinToken = regexp.MustCompile(`^K10[0-9a-f]{64}::[a-z0-9]{6}\.[a-z0-9]{16}$`)

// joinName is what the token's description may name: the joining node.
var joinName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// K3sJoinTokens has the agent mint join tokens with the k3s binary at path.
// Unset, it refuses to.
func (a *Agent) K3sJoinTokens(path string) {
	if path == "" {
		return
	}
	a.mintJoin = func(ctx context.Context, ttl time.Duration, description string) (string, error) {
		out, err := exec.CommandContext(ctx, path, "token", "create",
			"--ttl", ttl.String(), "--description", description).Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return "", fmt.Errorf("k3s token create: %v: %s", err, strings.TrimSpace(string(ee.Stderr)))
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
}

func (a *Agent) mintJoinToken(ctx context.Context, body io.Reader) (*appk8s.JoinToken, string, error) {
	if a.mintJoin == nil {
		return nil, "", errRefused{"a join token, which this cluster agent is not set up to mint"}
	}
	var req appk8s.JoinTokenRequest
	dec := json.NewDecoder(io.LimitReader(body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, "", errRefused{"a join token request that cannot be read: " + err.Error()}
	}
	if !joinName.MatchString(req.Node) {
		return nil, "", errRefused{fmt.Sprintf("a join token for %q, which is not a node name", req.Node)}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	token, err := a.mintJoin(ctx, JoinTokenTTL, "meshploy: "+req.Node+" joins")
	if err != nil {
		return nil, "", err
	}
	if !joinToken.MatchString(token) {
		return nil, "", errors.New("k3s gave a join token of an unknown shape")
	}
	return &appk8s.JoinToken{Token: token, ExpiresAt: a.timeNow().Add(JoinTokenTTL).UTC()},
		"a join token for " + req.Node + ", for " + JoinTokenTTL.String(), nil
}
