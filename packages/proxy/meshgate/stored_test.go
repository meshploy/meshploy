package meshgate

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/saved"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A saved snapshot decides as the one it was saved from.
func TestASavedSnapshotDecidesTheSame(t *testing.T) {
	w := newWorld()
	w.grant(db.ResourceStack, w.stack)
	w.setReach(db.ResourceStack, w.stack, false)
	w.setReach(db.ResourceService, w.app, true)
	w.s.rules = []db.MeshRule{{OrganizationID: w.org, FromKind: db.MeshRuleFromMachine, FromID: &w.laptopID, ToNodeID: w.gatewayID, Ports: "443"}}

	raw, err := json.Marshal(w.s)
	if err != nil {
		t.Fatal(err)
	}
	back := &Snapshot{}
	if err := json.Unmarshal(raw, back); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name      string
		got, want any
	}{
		{"machines", back.machines, w.s.machines},
		{"roles", back.roles, w.s.roles},
		{"grants", back.grants, w.s.grants},
		{"reach", back.reach, w.s.reach},
		{"services", back.services, w.s.services},
		{"levels", back.parent, w.s.parent},
		{"gateways", back.gateways, w.s.gateways},
	} {
		if !reflect.DeepEqual(field.got, field.want) {
			t.Errorf("%s: got %v, want %v", field.name, field.got, field.want)
		}
	}
	if !back.Enforced || len(back.rules) != 1 || *back.rules[0].FromID != w.laptopID {
		t.Errorf("enforced %v, rules %+v", back.Enforced, back.rules)
	}
	for addr := range w.s.machines {
		for _, to := range []Target{w.to(w.app), w.to(w.database), {RouteID: w.route, ProjectID: w.level, OrgID: w.org}} {
			if a, b := w.s.Decide(addr, to), back.Decide(addr, to); a != b {
				t.Errorf("%s to %v: saved decides %+v, loaded %+v", addr, to, b, a)
			}
		}
	}
}

// A gate whose database never answers decides from what was saved, and
// stays closed without it.
func TestAGateWithoutItsDatabase(t *testing.T) {
	down, err := gorm.Open(postgres.Open("postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := saved.Open(filepath.Join(t.TempDir(), "edge"))
	if err != nil {
		t.Fatal(err)
	}
	w := newWorld()
	w.grant(db.ResourceService, w.app)
	member := "100.64.0.5"

	g := New(down)
	g.Keep(dir)
	g.Start(time.Hour)
	if g.Check(member, w.to(w.app)).Allowed {
		t.Fatal("nothing saved, yet the gate opened to a member")
	}

	if err := dir.Save(savedName, w.s); err != nil {
		t.Fatal(err)
	}
	g = New(down)
	g.Keep(dir)
	g.Start(time.Hour)
	if v := g.Check(member, w.to(w.app)); !v.Allowed {
		t.Errorf("the saved grant was not honoured: %s", v.Reason)
	}
	if v := g.Check(member, w.to(w.database)); v.Allowed {
		t.Errorf("the saved state opened what it does not grant: %s", v.Reason)
	}
}
