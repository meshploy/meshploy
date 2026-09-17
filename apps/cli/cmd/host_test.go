package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// fakeHost stands in for the commands the agent runs: which exist, and what
// each prints.
func fakeHost(t *testing.T, present []string, outputs map[string]string) {
	t.Helper()
	origExec, origLook := hostExec, hostLookPath
	t.Cleanup(func() { hostExec, hostLookPath = origExec, origLook })
	hostLookPath = func(name string) (string, error) {
		for _, p := range present {
			if p == name {
				return "/usr/sbin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	hostExec = func(name string, args ...string) (string, error) {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		out, ok := outputs[key]
		if !ok {
			return "", errors.New("unexpected command: " + key)
		}
		return out, nil
	}
}

func TestCollectFirewallReadsActiveUFW(t *testing.T) {
	fakeHost(t, []string{"ufw", "iptables"}, map[string]string{
		"ufw status verbose": "Status: active\nDefault: deny (incoming), allow (outgoing), allow (routed)\n\nTo                         Action      From\n--                         ------      ----\n22/tcp                     ALLOW IN    Anywhere\n",
	})
	fw := collectFirewall()
	if fw.Tool != hostagent.ToolUFW || !fw.Active || len(fw.Rules) != 1 || fw.CheckedAt.IsZero() {
		t.Fatalf("got %+v", fw)
	}
}

// tcg-hg-2: ufw installed and off, iptables accepting, with Kubernetes' jumps.
func TestCollectFirewallInactiveUFWAndAcceptingIptablesIsNone(t *testing.T) {
	fakeHost(t, []string{"ufw", "iptables"}, map[string]string{
		"ufw status verbose": "Status: inactive\n",
		"iptables -S INPUT": "-P INPUT ACCEPT\n" +
			"-A INPUT -m comment --comment \"kube-router netpol - 4IA2OSFRMVNDXBVV\" -j KUBE-ROUTER-INPUT\n" +
			"-A INPUT -s 10.88.0.0/16 -p tcp -m tcp --dport 9100 -j ACCEPT\n",
	})
	if fw := collectFirewall(); fw.Tool != hostagent.ToolNone || fw.Active {
		t.Fatalf("got %+v, want none", fw)
	}
}

// A hand-written iptables firewall is not interpreted, so no port is called open.
func TestCollectFirewallDroppingIptablesIsUnparsed(t *testing.T) {
	fakeHost(t, []string{"iptables"}, map[string]string{
		"iptables -S INPUT": "-P INPUT DROP\n-A INPUT -p tcp --dport 22 -j ACCEPT\n",
	})
	fw := collectFirewall()
	if fw.Tool != hostagent.ToolIptables || !fw.Unparsed {
		t.Fatalf("got %+v", fw)
	}
	agent := &hostagent.Agent{HeartbeatAt: fw.CheckedAt}
	if v := hostagent.Evaluate(agent, &fw, 10000, "tcp", fw.CheckedAt); v.State != hostagent.PortUnknown {
		t.Errorf("port under an uninterpreted firewall: %s", v.State)
	}
}

func TestCollectFirewallReadsFirewalld(t *testing.T) {
	fakeHost(t, []string{"firewall-cmd"}, map[string]string{
		"firewall-cmd --state":                  "running\n",
		"firewall-cmd --get-default-zone":       "public\n",
		"firewall-cmd --zone=public --list-all": "public (active)\n  target: default\n  services: ssh\n  ports: 443/tcp\n  rich rules:\n",
		"firewall-cmd --info-service=ssh":       "ssh\n  ports: 22/tcp\n  protocols:\n",
	})
	fw := collectFirewall()
	if fw.Tool != hostagent.ToolFirewalld || len(fw.Rules) != 2 || fw.Rules[0].Ports != "22" {
		t.Fatalf("got %+v", fw)
	}
}

func TestCollectFirewallRecordsAFailedCheck(t *testing.T) {
	fakeHost(t, []string{"ufw"}, nil)
	if fw := collectFirewall(); fw.Error == "" || fw.Tool != hostagent.ToolUFW {
		t.Fatalf("got %+v, want the failure recorded", fw)
	}
}

// One pass of the agent writes both reports where the API reads them.
func TestHostServeWritesReports(t *testing.T) {
	fakeHost(t, nil, nil)
	orig := hostDir
	hostDir = t.TempDir()
	t.Cleanup(func() { hostDir = orig })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- hostServe(ctx, os.Stderr) }()
	deadline := time.Now().Add(2 * time.Second)
	for !fileExists(filepath.Join(hostagent.StateDir(hostDir), hostagent.AgentFile)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	agent, fw, err := hostagent.ReadState(hostDir)
	if err != nil || agent == nil || fw == nil {
		t.Fatalf("agent=%v fw=%v err=%v", agent, fw, err)
	}
	if !agent.Tasks["firewall"].OK || fw.Tool != hostagent.ToolNone {
		t.Errorf("agent=%+v fw=%+v", agent, fw)
	}
}

func TestHostStartInstallsAndStartsTheUnit(t *testing.T) {
	fx := newUpdaterFixture(t)
	orig := hostDir
	hostDir = filepath.Join(fx.root, "host")
	t.Cleanup(func() { hostDir = orig })

	if err := hostStart(os.Stdout); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(systemdUnitDir, hostUnit))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ExecStart=" + updaterCLIPath + " host serve"; !strings.Contains(string(unit), want) {
		t.Errorf("unit lacks %q:\n%s", want, unit)
	}
	if !fileExists(hostagent.StateDir(hostDir)) {
		t.Error("the state directory the API mounts was not created")
	}
	got := make([]string, len(fx.ctl))
	for i, c := range fx.ctl {
		got[i] = strings.Join(c, " ")
	}
	want := []string{"daemon-reload", "enable " + hostUnit, "restart " + hostUnit}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", got, want)
	}
}
