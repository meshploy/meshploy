package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LoginInfo is what a server says about itself to a CLI finding it.
type LoginInfo struct {
	ConsoleURL string `json:"console_url"`
	APIURL     string `json:"api_url"`
	Version    string `json:"version"`
}

// LoginInfo asks the server for its addresses. It needs no credential, and
// answering it is how a CLI knows it found a Meshploy server.
func (c *Client) LoginInfo() (LoginInfo, error) {
	resp, err := c.do("GET", "/api/v1/system/login-info", nil)
	if err != nil {
		return LoginInfo{}, err
	}
	return decode[LoginInfo](resp)
}

// CLILogin is a login started for this machine, waiting to be approved in a
// browser. DeviceCode is the CLI's own secret, to poll with.
type CLILogin struct {
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	VerificationURL string    `json:"verification_url"`
	APIURL          string    `json:"api_url"`
	Interval        int       `json:"interval"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type startCLILoginInput struct {
	Host string `json:"host"`
	Door string `json:"door,omitempty"`
}

// StartCLILogin begins a login for the machine named host, to be approved at
// door (the console when empty).
func (c *Client) StartCLILogin(host, door string) (CLILogin, error) {
	resp, err := c.do("POST", "/api/v1/cli/logins", startCLILoginInput{Host: host, Door: door})
	if err != nil {
		return CLILogin{}, err
	}
	return decode[CLILogin](resp)
}

// CLILoginStatus is the state of a login: pending, approved (with the token,
// once), denied, collected or expired.
type CLILoginStatus struct {
	Status string `json:"status"`
	Token  string `json:"token,omitempty"`
}

// ErrSlowDown is a poll the server asked to be made less often.
var ErrSlowDown = errors.New("polling too fast")

// PollCLILogin asks whether a login was approved.
func (c *Client) PollCLILogin(deviceCode string) (CLILoginStatus, error) {
	resp, err := c.do("POST", "/api/v1/cli/logins/token", map[string]string{"device_code": deviceCode})
	if err != nil {
		return CLILoginStatus{}, err
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		resp.Body.Close()
		return CLILoginStatus{}, ErrSlowDown
	case http.StatusNotFound:
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return CLILoginStatus{}, fmt.Errorf("the server no longer knows this login: %s", b)
	}
	return decode[CLILoginStatus](resp)
}

// LogoutCLI revokes the CLI token this client holds.
func (c *Client) LogoutCLI() error {
	return c.doNoContent("DELETE", "/api/v1/cli/session")
}
