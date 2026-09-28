package mesh

import (
	"errors"
	"strings"
	"testing"
)

type fakeFirewall struct {
	chains map[string]string // tool -> INPUT listing; missing = no mp-input
	ran    []string
}

func (f *fakeFirewall) run(name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	f.ran = append(f.ran, cmd)
	switch {
	case strings.HasSuffix(cmd, "-S mp-input"):
		if _, ok := f.chains[name]; !ok {
			return "", errors.New("no chain")
		}
		return "-N mp-input\n", nil
	case strings.HasSuffix(cmd, "-S INPUT"):
		return f.chains[name], nil
	case strings.Contains(cmd, "-D INPUT -j mp-input"):
		if strings.Contains(f.chains[name], "-j mp-input") {
			f.chains[name] = strings.Replace(f.chains[name], "-A INPUT -j mp-input\n", "", 1)
			return "", nil
		}
		return "", errors.New("no such rule")
	}
	return "", nil
}

// The stock Tailscale's hook, put back on top by its restart, is overtaken
// again; a mesh hook already first is left alone; a machine with no mesh
// chain is not touched.
func TestKeepFirstPutsTheMeshHookInFront(t *testing.T) {
	f := &fakeFirewall{chains: map[string]string{
		"iptables":  "-P INPUT ACCEPT\n-A INPUT -j ts-input\n-A INPUT -j mp-input\n",
		"ip6tables": "-P INPUT ACCEPT\n-A INPUT -j mp-input\n-A INPUT -j ts-input\n",
	}}
	moved := KeepFirst(f.run)
	if len(moved) != 1 || !strings.HasPrefix(moved[0], "iptables:") {
		t.Fatalf("moved = %v", moved)
	}
	want := "iptables -I INPUT 1 -j mp-input"
	found := false
	for _, c := range f.ran {
		if c == want {
			found = true
		}
		if strings.HasPrefix(c, "ip6tables -I") {
			t.Errorf("IPv6 was already in order, and was changed: %s", c)
		}
	}
	if !found {
		t.Errorf("never ran %q: %v", want, f.ran)
	}

	// kube-router's hook ahead of the mesh's is left alone: it drops nothing
	// of the mesh's, and it puts itself back on top every sync.
	kube := &fakeFirewall{chains: map[string]string{
		"iptables": "-P INPUT ACCEPT\n-A INPUT -j KUBE-ROUTER-INPUT\n-A INPUT -j mp-input\n-A INPUT -j ts-input\n",
	}}
	if moved := KeepFirst(kube.run); len(moved) != 0 {
		t.Errorf("moved ahead of kube-router: %v", moved)
	}

	none := &fakeFirewall{chains: map[string]string{}}
	if moved := KeepFirst(none.run); len(moved) != 0 {
		t.Errorf("no mesh chain, yet moved %v", moved)
	}
}

// The unit runs the mesh on its own interface, socket, state and port, and
// never reports to Tailscale's servers.
func TestUnitFileKeepsTheMeshApart(t *testing.T) {
	u := UnitFile()
	for _, want := range []string{"--tun=meshploy0", "--socket=" + Socket, "--statedir=" + StateDir, "--port=41642", "--no-logs-no-support"} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %s", want)
		}
	}
}
