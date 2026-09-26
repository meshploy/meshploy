package dokploy

import "testing"

// Answering a group's question lets it move; leaving an app on Dokploy holds
// its group when it shares data, and marks it not moved.
func TestSettleAppliesAnswersToGroups(t *testing.T) {
	ask := Decision{ID: "compose:agent:host-network", Question: "agent uses the host's network",
		Options: []Option{{"move_without", "Move"}, optLeaveOnDokploy}}
	p := Plan{
		Items: []Item{
			{ID: "c1", Name: "monitoring", Verdict: NeedsYou, Decisions: []Decision{ask}},
			{ID: "a1", Name: "api", Verdict: Moves},
			{ID: "d1", Name: "db", Verdict: Moves},
			{ID: "c2", Name: "legacy", Verdict: NeedsYou, Decisions: []Decision{ask}},
		},
		Groups: []Group{
			{ID: "g1", Name: "monitoring", Members: []GroupMember{{ID: "c1", Name: "monitoring"}}, CanMove: false, Blockers: []string{"monitoring: agent uses the host's network"}},
			{ID: "g2", Name: "shop", Members: []GroupMember{{ID: "a1", Name: "api"}, {ID: "d1", Name: "db"}}, CanMove: true},
			{ID: "g3", Name: "legacy", Members: []GroupMember{{ID: "c2", Name: "legacy"}}, CanMove: false},
		},
	}
	s := Settle(p, map[string]map[string]string{
		"c1": {"compose:agent:host-network": "move_without"},
		"c2": {"compose:agent:host-network": "leave"},
	}, []string{"a1"})

	if !s.Groups[0].CanMove || len(s.Groups[0].Blockers) != 0 {
		t.Errorf("answered: should move, %+v", s.Groups[0])
	}
	if s.Groups[1].CanMove {
		t.Errorf("api excluded while db moves would split their data: %+v", s.Groups[1])
	}
	if s.Groups[2].CanMove || s.Items[3].Verdict != NotMoved {
		t.Errorf("left on Dokploy: %+v %+v", s.Groups[2], s.Items[3])
	}
	if p.Groups[0].CanMove {
		t.Error("the plan passed in must not change")
	}
}
