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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// AppURL is the account plane's web address, where a person approves
	// agent apps acting as them (https://app.meshploy.com).
	AppURL string `json:"app_url,omitempty"`
	// Managed: the account plane runs this install for its people, as it
	// does a Meshploy Cloud workspace, and its people sign in through it. A
	// call then names its person by their verified email, and the install
	// acts as its own user with that email; no one links an account.
	Managed bool `json:"managed,omitempty"`
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

	// TypePersonLinked goes from the account plane when a person approved
	// agent apps acting as them on this install; it carries the install's
	// one-time code back, signed.
	TypePersonLinked = "person_linked"
	// TypePersonUnlinked goes either way: the account plane when the person
	// withdrew it there, the install when they withdrew it on the install.
	TypePersonUnlinked = "person_unlinked"
	// TypeLinkOwner goes from the install when the person who linked it turned
	// agent apps on: it carries a one-time code, and the account plane answers
	// with person_linked for the account the install is linked to, with no
	// approval page, since that account made the link.
	TypeLinkOwner = "link_owner"

	// TypeCall goes from the account plane: one request of an agent app,
	// for the install to run as the person it acts for. Signed.
	TypeCall = "call"
	// TypeResult goes from the install: the call's answer.
	TypeResult = "result"
)

// Message is one frame on the connection.
type Message struct {
	Type   string  `json:"type"`
	Report *Report `json:"report,omitempty"`
	Person *Person `json:"person,omitempty"`
	Call   *Call   `json:"call,omitempty"`
	Result *Result `json:"result,omitempty"`
}

// Person links an account-plane account to a person on the install.
type Person struct {
	// Code is the one-time code the install made when its person asked to
	// connect their account; empty on person_unlinked.
	Code string `json:"code,omitempty"`
	// Account is the account plane's id for the account; Email names it.
	Account string `json:"account"`
	Email   string `json:"email,omitempty"`
	// Signature is the account plane's, over PersonString; set on
	// person_linked and on person_unlinked from the account plane.
	Signature string `json:"signature,omitempty"`
}

// PersonString is what the account plane signs on a person frame.
func PersonString(kind, linkID string, p Person) []byte {
	return []byte("meshploy-fleet-person\n" + kind + "\n" + linkID + "\n" + p.Code + "\n" + p.Account + "\n" + p.Email)
}

// SignPerson signs a person frame for linkID.
func SignPerson(kind, linkID string, p *Person, key ed25519.PrivateKey) {
	p.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, PersonString(kind, linkID, *p)))
}

// VerifyPerson checks a person frame against the account plane's key.
func VerifyPerson(kind, linkID string, p Person, pub ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil || !ed25519.Verify(pub, PersonString(kind, linkID, p), sig) {
		return errors.New("bad signature")
	}
	return nil
}

// Call is one request an agent app makes through the account plane: the
// install runs it against its own API as the person the account is linked to.
type Call struct {
	ID string `json:"id"`
	// Account is the account plane's id of the account the app acts for.
	Account string `json:"account"`
	// Email is the account's verified address, sent to a managed install
	// only, which acts as its user with that address.
	Email string `json:"email,omitempty"`
	// App names the agent app, as the person approved it ("Claude").
	App    string `json:"app"`
	Method string `json:"method"`
	// Path is the API path with its query, under /api/v1/.
	Path        string    `json:"path"`
	ContentType string    `json:"content_type,omitempty"`
	Body        []byte    `json:"body,omitempty"`
	Expires     time.Time `json:"expires"`
	Signature   string    `json:"signature"`
}

// CallTTL is how long a call may take to arrive.
const CallTTL = time.Minute

// CallString is what the account plane signs on a call. The email is
// signed when there is one, so a call without it reads as it always has.
func CallString(linkID string, c Call) []byte {
	sum := sha256.Sum256(c.Body)
	s := "meshploy-fleet-call\n" + linkID + "\n" + c.ID + "\n" + c.Account + "\n" + c.App + "\n" +
		c.Method + "\n" + c.Path + "\n" + c.ContentType + "\n" + hex.EncodeToString(sum[:]) + "\n" +
		strconv.FormatInt(c.Expires.Unix(), 10)
	if c.Email != "" {
		s += "\nemail:" + c.Email
	}
	return []byte(s)
}

// SignCall signs a call for linkID.
func SignCall(linkID string, c *Call, key ed25519.PrivateKey) {
	c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, CallString(linkID, *c)))
}

// VerifyCall checks a call's signature and that it has not expired.
func VerifyCall(linkID string, c Call, pub ed25519.PublicKey, now time.Time) error {
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil || !ed25519.Verify(pub, CallString(linkID, c), sig) {
		return errors.New("bad signature")
	}
	if now.After(c.Expires) || c.Expires.Sub(now) > CallTTL+MaxClockSkew {
		return errors.New("the call has expired")
	}
	return nil
}

// Result is the install's answer to a call.
type Result struct {
	ID          string `json:"id"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
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
	// Members are the addresses of the people who sign in here, sent by a
	// managed install only: who the account plane's agent apps reach it for.
	Members []string `json:"members,omitempty"`
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
