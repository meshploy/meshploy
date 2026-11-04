package meshgate

import (
	"encoding/json"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// storedSnapshot is a Snapshot as it is saved: its maps as lists, since JSON
// keys are strings and these are pairs of IDs.
type storedSnapshot struct {
	Enforced bool            `json:"enforced"`
	Machines []storedMachine `json:"machines"`
	Roles    []storedRole    `json:"roles"`
	Grants   []storedGrant   `json:"grants"`
	Reach    []storedGrant   `json:"reach"`
	Services []storedService `json:"services"`
	Levels   []storedLevel   `json:"levels"`
	Rules    []db.MeshRule   `json:"rules"`
	Gateways []uuid.UUID     `json:"gateways"`
}

type storedMachine struct {
	Addr  string     `json:"addr"`
	ID    uuid.UUID  `json:"id"`
	Org   uuid.UUID  `json:"org"`
	Name  string     `json:"name"`
	Kind  string     `json:"kind"`
	Owner *uuid.UUID `json:"owner,omitempty"`
}

type storedRole struct {
	Org  uuid.UUID     `json:"org"`
	User uuid.UUID     `json:"user"`
	Role db.MemberRole `json:"role"`
}

// storedGrant is a grant, or a reach switch with On set as it was chosen.
type storedGrant struct {
	Org  uuid.UUID       `json:"org"`
	User uuid.UUID       `json:"user"`
	Type db.ResourceType `json:"type"`
	ID   uuid.UUID       `json:"id"`
	On   bool            `json:"on,omitempty"`
}

type storedService struct {
	ID       uuid.UUID  `json:"id"`
	Project  uuid.UUID  `json:"project"`
	Stack    *uuid.UUID `json:"stack,omitempty"`
	Database bool       `json:"database,omitempty"`
}

type storedLevel struct {
	ID      uuid.UUID `json:"id"`
	Project uuid.UUID `json:"project"`
}

func (s *Snapshot) MarshalJSON() ([]byte, error) {
	out := storedSnapshot{Enforced: s.Enforced, Rules: s.rules}
	for addr, m := range s.machines {
		out.Machines = append(out.Machines, storedMachine{Addr: addr, ID: m.id, Org: m.org, Name: m.name, Kind: m.kind, Owner: m.owner})
	}
	for k, r := range s.roles {
		out.Roles = append(out.Roles, storedRole{Org: k.org, User: k.user, Role: r})
	}
	for k, gs := range s.grants {
		for g, on := range gs {
			if on {
				out.Grants = append(out.Grants, storedGrant{Org: k.org, User: k.user, Type: g.t, ID: g.id})
			}
		}
	}
	for k, gs := range s.reach {
		for g, on := range gs {
			out.Reach = append(out.Reach, storedGrant{Org: k.org, User: k.user, Type: g.t, ID: g.id, On: on})
		}
	}
	for id, svc := range s.services {
		out.Services = append(out.Services, storedService{ID: id, Project: svc.project, Stack: svc.stack, Database: svc.database})
	}
	for id, p := range s.parent {
		out.Levels = append(out.Levels, storedLevel{ID: id, Project: p})
	}
	for id, on := range s.gateways {
		if on {
			out.Gateways = append(out.Gateways, id)
		}
	}
	return json.Marshal(out)
}

func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var in storedSnapshot
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*s = Snapshot{
		Enforced: in.Enforced,
		machines: map[string]machine{},
		roles:    map[memberKey]db.MemberRole{},
		grants:   map[memberKey]map[grantKey]bool{},
		reach:    map[memberKey]map[grantKey]bool{},
		services: map[uuid.UUID]service{},
		parent:   map[uuid.UUID]uuid.UUID{},
		rules:    in.Rules,
		gateways: map[uuid.UUID]bool{},
	}
	for _, m := range in.Machines {
		s.machines[m.Addr] = machine{id: m.ID, org: m.Org, name: m.Name, kind: m.Kind, owner: m.Owner}
	}
	for _, r := range in.Roles {
		s.roles[memberKey{r.Org, r.User}] = r.Role
	}
	for _, g := range in.Grants {
		k := memberKey{g.Org, g.User}
		if s.grants[k] == nil {
			s.grants[k] = map[grantKey]bool{}
		}
		s.grants[k][grantKey{g.Type, g.ID}] = true
	}
	for _, g := range in.Reach {
		k := memberKey{g.Org, g.User}
		if s.reach[k] == nil {
			s.reach[k] = map[grantKey]bool{}
		}
		s.reach[k][grantKey{g.Type, g.ID}] = g.On
	}
	for _, svc := range in.Services {
		s.services[svc.ID] = service{project: svc.Project, stack: svc.Stack, database: svc.Database}
	}
	for _, l := range in.Levels {
		s.parent[l.ID] = l.Project
	}
	for _, id := range in.Gateways {
		s.gateways[id] = true
	}
	return nil
}
