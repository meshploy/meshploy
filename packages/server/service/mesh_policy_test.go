package service_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// A small install: a gateway, a worker, an admin's laptop, a member's laptop
// granted one database, a laptop nobody owns, and a machine of another
// organisation's member.
func meshFixture() service.MeshInputs {
	org := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	other := uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	admin := uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	member := uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	stranger := uuid.MustParse("00000000-0000-0000-0000-0000000000b3")
	id := func(n string) uuid.UUID { return uuid.MustParse("00000000-0000-0000-0000-0000000000" + n) }
	return service.MeshInputs{
		Nodes: []service.MeshNode{
			{ID: id("c1"), OrgID: org, Name: "gw-1", IP: "100.64.0.1", Kind: service.MeshGateway},
			{ID: id("c2"), OrgID: org, Name: "worker-1", IP: "100.64.0.2", Kind: service.MeshCluster},
			{ID: id("c3"), OrgID: org, Name: "Asha's MacBook", IP: "100.64.0.3", Kind: service.MeshConnected, OwnerID: &admin},
			{ID: id("c4"), OrgID: org, Name: "ravi-laptop", IP: "100.64.0.4", Kind: service.MeshConnected, OwnerID: &member},
			{ID: id("c5"), OrgID: org, Name: "old-box", IP: "100.64.0.5", Kind: service.MeshConnected},
			{ID: id("c6"), OrgID: other, Name: "elsewhere", IP: "100.64.0.6", Kind: service.MeshConnected, OwnerID: &stranger},
			{ID: id("c7"), OrgID: org, Name: "joining", Kind: service.MeshCluster},
		},
		Members: []service.MeshMember{
			{OrgID: org, UserID: admin, Name: "asha", Role: meshdb.RoleAdmin},
			{OrgID: org, UserID: member, Name: "ravi", Role: meshdb.RoleMember},
			{OrgID: other, UserID: stranger, Name: "sam", Role: meshdb.RoleMember},
		},
		Grants: []service.MeshGrant{
			{UserID: member, What: "postgres in shop", NodePorts: []int{31432}, GatewayPorts: []int{5433}},
		},
	}
}

// The rendered policy is a checked-in file a reviewer can read: a change to
// what the mesh allows shows as a diff of it.
func TestTheMeshPolicyRendersAsReviewed(t *testing.T) {
	got, err := service.BuildMeshPolicy(meshFixture()).Headscale()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "mesh_policy.golden.json")
	if *updateGolden {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
		t.Errorf("the policy changed; review it and run with -update:\n%s", got)
	}
}

// What each machine may reach, as the report says it.
func TestEachMachineReachesWhatItsOwnerMayUse(t *testing.T) {
	p := service.BuildMeshPolicy(meshFixture())
	byName := map[string]service.MeshMachine{}
	for _, m := range p.Machines {
		byName[m.Name] = m
	}
	reaches := func(machine, to string, port int) bool {
		for _, r := range byName[machine].Reaches {
			if r.To == "every machine" || strings.Contains(r.To, to) {
				if len(r.Ports) == 0 && r.To == "every machine" {
					return true
				}
				for _, p := range r.Ports {
					if p == port {
						return true
					}
				}
				if len(r.Ports) == 0 && strings.Contains(r.To, to) {
					return true
				}
			}
		}
		return false
	}

	if !byName["Asha's MacBook"].Everything || byName["Asha's MacBook"].OwnerName != "asha" {
		t.Errorf("an admin's machine should reach everything: %+v", byName["Asha's MacBook"])
	}
	// Everything is their own organisation's: never another's machines.
	for _, r := range p.Rules {
		if contains(r.From, hostOf(p, "Asha's MacBook")) && containsDest(r.To, hostOf(p, "elsewhere")) {
			t.Errorf("an admin's machine reaches another organisation's: %+v", r)
		}
	}
	if byName["ravi-laptop"].Everything {
		t.Error("a member's machine reaches everything")
	}
	if !reaches("ravi-laptop", "worker-1", 31432) || !reaches("ravi-laptop", "gw-1", 5433) {
		t.Errorf("a member's machine should reach its database's ports: %+v", byName["ravi-laptop"].Reaches)
	}
	if reaches("ravi-laptop", "worker-1", 22) || reaches("ravi-laptop", "worker-1", 5432) {
		t.Errorf("a member's machine reaches other ports on the database's machines: %+v", byName["ravi-laptop"].Reaches)
	}
	if reaches("old-box", "worker-1", 31432) || !reaches("old-box", "gw-1", 53) {
		t.Errorf("a machine with no owner should reach only the gateway's basics: %+v", byName["old-box"].Reaches)
	}
	if !reaches("worker-1", "gw-1", 6443) || !reaches("gw-1", "ravi-laptop", 22) {
		t.Error("the cluster's own traffic is not allowed")
	}
	// Another organisation's member, granted nothing here, reaches none of it.
	if reaches("elsewhere", "worker-1", 31432) || byName["elsewhere"].Everything {
		t.Errorf("another organisation's machine reaches this one's: %+v", byName["elsewhere"].Reaches)
	}
	if hostOf(p, "joining") != "" {
		t.Error("a machine not yet on the mesh was given an address")
	}
}

func hostOf(p service.MeshPolicy, name string) string {
	for _, m := range p.Machines {
		if m.Name == name && m.IP != "" {
			for h, ip := range p.Hosts {
				if ip == m.IP+"/32" {
					return h
				}
			}
		}
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func containsDest(ds []service.MeshDest, host string) bool {
	for _, d := range ds {
		if d.Host == host {
			return true
		}
	}
	return false
}

// A machine joining is unknown until it registers, and registers through the
// gateway's API over the mesh: every machine may reach the gateway's DNS and
// API, whether Meshploy knows it or not.
func TestAMachineNotYetRegisteredCanRegister(t *testing.T) {
	b, err := service.BuildMeshPolicy(meshFixture()).Headscale()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"src": [
        "*"
      ],
      "dst": [
        "gw-1-00c1:53",
        "gw-1-00c1:4000"`) {
		t.Errorf("the gateway's DNS and API are not open to every machine:\n%s", b)
	}
}
