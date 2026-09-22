package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The install script is served with this gateway's own address written into it,
// which is what lets the install command carry nothing but a token.
func TestInstallScriptCarriesTheGatewaysAddress(t *testing.T) {
	script := []byte("#!/usr/bin/env bash\n" + apiBaseLine + "\necho hello\n")

	t.Run("filled in when the gateway knows where it answers", func(t *testing.T) {
		got := string(withAPIBase(script, "https://api.example.com"))
		want := `MESHPLOY_API_BASE="${MESHPLOY_API_BASE:-https://api.example.com}"`
		if !strings.Contains(got, want) {
			t.Fatalf("not substituted:\n%s", got)
		}
		// The environment still wins, so an operator can point a machine
		// somewhere else without editing the script.
		if !strings.Contains(got, "${MESHPLOY_API_BASE:-") {
			t.Error("the substitution must stay a default, not a hardcoding")
		}
	})

	// A gateway that does not know its own address must leave the line alone
	// rather than write an empty or local one into it: the script then asks for
	// --api and says so, which is a better failure than a machine quietly
	// trying to provision against http://localhost:4000.
	t.Run("left alone when it does not", func(t *testing.T) {
		if got := string(withAPIBase(script, "")); got != string(script) {
			t.Fatalf("changed:\n%s", got)
		}
	})

	// If install.sh ever stops carrying the line, substitution stops rather
	// than putting the address somewhere it was not meant to go.
	t.Run("left alone when the script does not have the line", func(t *testing.T) {
		other := []byte("#!/usr/bin/env bash\necho nothing to fill\n")
		if got := string(withAPIBase(other, "https://api.example.com")); got != string(other) {
			t.Fatalf("changed a script with no placeholder:\n%s", got)
		}
	})
}

// The real deploy/install.sh must carry the line the handler looks for. These
// two live in different languages and different directories, and nothing but a
// test keeps them agreeing.
func TestTheRealInstallScriptStillHasThePlaceholder(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "install.sh")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("deploy/install.sh not readable from here: %v", err)
	}
	if !strings.Contains(string(body), apiBaseLine) {
		t.Fatalf("deploy/install.sh no longer contains %q, so the gateway cannot write its address into it", apiBaseLine)
	}
	if !strings.Contains(string(body), "--token=*)") {
		t.Error("deploy/install.sh no longer accepts --token, which is the whole one-command path")
	}
}

// A one-command install must not stop at the last step.
//
// Registration is the moment the machine has proved it is on the mesh, and it
// is where the cluster's join token is handed over - the public provisioning
// call before it deliberately carries no such thing. Without this the installer
// reached the k3s step under --auto with nothing to answer its prompt and died,
// leaving a node on the mesh and out of the cluster.
func TestTheInstallerTakesTheJoinTokenFromRegistration(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "install.sh"))
	if err != nil {
		t.Skipf("deploy/install.sh not readable from here: %v", err)
	}
	script := string(body)

	if !strings.Contains(script, "k3s_token") {
		t.Error("install.sh never reads k3s_token from the registration response")
	}
	if !strings.Contains(script, "k3s_server_url") {
		t.Error("install.sh never reads k3s_server_url from the registration response")
	}
	// And when the gateway hands none back, it says so instead of prompting a
	// machine nobody is watching.
	if !strings.Contains(script, "No k3s join token came back from the gateway") {
		t.Error("install.sh has no answer for a provisioned node with no join token")
	}
}
