// Package fleet is the contract between a Meshploy install and the account
// plane it links to (Meshploy Cloud, or any console that speaks it): the link
// token an owner pastes, enrolment, the signed connection the install opens,
// and the messages sent over it.
//
// The install always dials out. Nothing on the account plane can open a
// connection to an install, so a link needs no inbound port and works behind
// NAT and on installs reachable only over their mesh. Each side holds its own
// Ed25519 key and keeps the other's public key from enrolment; neither ever
// holds a credential of the other's.
package fleet

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TokenPrefix marks a link token. The token carries the address of the
// account plane's connector as well as the secret, so pasting one string is
// the whole of enrolment, and a test account plane needs no setting on the
// install.
const TokenPrefix = "mflt-"

// Paths on the connector, relative to the token's URL.
const (
	EnrollPath  = "/v1/enroll"
	ConnectPath = "/v1/connect"
	// UnlinkPath is where an install says it unlinked, signed like a
	// connection, so the account plane can tell that from a server gone quiet.
	UnlinkPath = "/v1/unlink"
)

// Headers the install signs the connection with.
const (
	HeaderLink      = "X-Meshploy-Link"
	HeaderTime      = "X-Meshploy-Time"
	HeaderSignature = "X-Meshploy-Signature"
)

// MaxClockSkew is how far a connection's signed time may be from the
// receiver's clock.
const MaxClockSkew = 5 * time.Minute

// Token is a parsed link token.
type Token struct {
	// URL is the connector's base address, https:// (http:// is accepted for
	// a local account plane on a loopback address only).
	URL    string `json:"u"`
	Secret string `json:"s"`
}

// String formats the token for pasting.
func (t Token) String() string {
	b, _ := json.Marshal(t)
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// ParseToken reads a pasted token and checks its address.
func ParseToken(s string) (Token, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, TokenPrefix) {
		return Token{}, errors.New("this is not a link token: it should start with " + TokenPrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, TokenPrefix))
	if err != nil {
		return Token{}, errors.New("this link token is damaged: copy it again")
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil || t.URL == "" || t.Secret == "" {
		return Token{}, errors.New("this link token is damaged: copy it again")
	}
	u, err := url.Parse(t.URL)
	if err != nil || u.Host == "" {
		return Token{}, errors.New("this link token names no valid address")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return Token{}, fmt.Errorf("this link token points at %s, which is not https", t.URL)
	}
	t.URL = strings.TrimRight(t.URL, "/")
	return t, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// EnrollRequest is what an install sends, once, to redeem a token.
type EnrollRequest struct {
	Secret string `json:"secret"`
	// PublicKey is the install's Ed25519 public key, base64.
	PublicKey string `json:"public_key"`
	Install   About  `json:"install"`
}

// EnrollResponse is the account plane's answer.
type EnrollResponse struct {
	LinkID string `json:"link_id"`
	// PublicKey is the account plane's Ed25519 public key, base64. The
	// install pins it: everything the account plane sends later is signed
	// with it.
	PublicKey string `json:"public_key"`
	// Account names who the install is now linked to, for the install's
	// owner to see ("Linked to ada@example.com").
	Account string `json:"account"`
}

// About describes an install. Sent at enrolment and with every report.
type About struct {
	Name       string `json:"name"`        // the workspace's name
	ConsoleURL string `json:"console_url"` // where its console answers
	Version    string `json:"version"`
	Edition    string `json:"edition"` // community or enterprise
}

// ConnectString is what the install signs to open its connection.
func ConnectString(linkID string, unix int64) []byte {
	return []byte("meshploy-fleet-connect\n" + linkID + "\n" + strconv.FormatInt(unix, 10))
}

// SignConnect returns the three headers for a connection.
func SignConnect(linkID string, key ed25519.PrivateKey, now time.Time) map[string]string {
	unix := now.Unix()
	sig := ed25519.Sign(key, ConnectString(linkID, unix))
	return map[string]string{
		HeaderLink:      linkID,
		HeaderTime:      strconv.FormatInt(unix, 10),
		HeaderSignature: base64.StdEncoding.EncodeToString(sig),
	}
}

// VerifyConnect checks a connection's headers against the install's public
// key, as the account plane stored it at enrolment.
func VerifyConnect(linkID, unixText, sigText string, pub ed25519.PublicKey, now time.Time) error {
	unix, err := strconv.ParseInt(unixText, 10, 64)
	if err != nil {
		return errors.New("bad time")
	}
	if d := now.Sub(time.Unix(unix, 0)); d > MaxClockSkew || d < -MaxClockSkew {
		return errors.New("the install's clock is too far from ours")
	}
	sig, err := base64.StdEncoding.DecodeString(sigText)
	if err != nil || !ed25519.Verify(pub, ConnectString(linkID, unix), sig) {
		return errors.New("bad signature")
	}
	return nil
}

// Message types sent over the connection.
const (
	// TypeReport goes from the install: its state, while its owner has
	// reporting on.
	TypeReport = "report"
	// TypePaused goes from the install when its owner turned reporting off:
	// the link stands, and nothing about the install is sent.
	TypePaused = "paused"
	// TypeUnlinked goes from the account plane when the link was removed
	// there; the install forgets the link.
	TypeUnlinked = "unlinked"
)

// Message is one frame on the connection.
type Message struct {
	Type   string  `json:"type"`
	Report *Report `json:"report,omitempty"`
}

// Report is what an install tells its account plane about itself. Counts and
// states only: no names of projects or services, no addresses, no secrets.
type Report struct {
	About    About     `json:"about"`
	At       time.Time `json:"at"`
	Nodes    Count     `json:"nodes"` // online of total
	Projects int       `json:"projects"`
	Services Count     `json:"services"` // running of total
	// Backups: databases and volumes with a schedule, and how many of those
	// failed their last run.
	Backups BackupCounts `json:"backups"`
	// UpdateAvailable is the newest release, when it is newer than Version.
	UpdateAvailable string `json:"update_available,omitempty"`
}

// Count is n of total.
type Count struct {
	N     int `json:"n"`
	Total int `json:"total"`
}

// BackupCounts summarises scheduled backups.
type BackupCounts struct {
	Scheduled int `json:"scheduled"`
	Failing   int `json:"failing"`
	// Unprotected is databases with no backup schedule.
	Unprotected int `json:"unprotected"`
}
