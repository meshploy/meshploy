package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSetupToken = "ms_0123456789abcdef0123456789abcdef"

func showOutput(t *testing.T, token string, open func() (bool, error)) string {
	t.Helper()
	var b bytes.Buffer
	if err := printSetupToken(&b, token, open); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestShowPrintsTheTokenWhileUnclaimed(t *testing.T) {
	out := showOutput(t, testSetupToken, func() (bool, error) { return true, nil })
	if !strings.Contains(out, testSetupToken) {
		t.Fatalf("token missing from output:\n%s", out)
	}
}

// Once an owner exists the token is never accepted again; printing it would
// hand the operator a string that looks usable and is not.
func TestShowWithholdsTheTokenOnceClaimed(t *testing.T) {
	out := showOutput(t, testSetupToken, func() (bool, error) { return false, nil })
	if strings.Contains(out, testSetupToken) {
		t.Fatalf("printed a token that can no longer be used:\n%s", out)
	}
	if !strings.Contains(out, "already has an owner") {
		t.Fatalf("no explanation of why nothing was printed:\n%s", out)
	}
}

// An unreachable API is usually an install still coming up, which is exactly
// when the token is needed.
func TestShowPrintsTheTokenWhenTheAPIIsUnreachable(t *testing.T) {
	out := showOutput(t, testSetupToken, func() (bool, error) { return false, errors.New("connection refused") })
	if !strings.Contains(out, testSetupToken) {
		t.Fatalf("withheld the token because the API was down:\n%s", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("did not say the ownership check failed:\n%s", out)
	}
}

func TestShowWithoutATokenSaysSo(t *testing.T) {
	asked := false
	out := showOutput(t, "", func() (bool, error) { asked = true; return true, nil })
	if !strings.Contains(out, "no setup token configured") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if asked {
		t.Fatal("asked the API about a token that does not exist")
	}
}

func TestRegistrationOpenReadsTheAPI(t *testing.T) {
	for _, c := range []struct {
		name    string
		code    int
		body    string
		want    bool
		wantErr bool
	}{
		{"open", 200, `{"registration_open":true,"setup_required":false}`, true, false},
		{"claimed", 200, `{"registration_open":false,"setup_required":false}`, false, false},
		// Missing must not read as "claimed", or the token is withheld from
		// the operator who needs it.
		{"field missing", 200, `{"setup_required":false}`, false, true},
		{"server error", 500, `{}`, false, true},
		{"not json", 200, `<html>`, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/auth/status" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(c.code)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			got, err := registrationOpen(srv.URL)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("registrationOpen = %v, want %v", got, c.want)
			}
		})
	}
}
