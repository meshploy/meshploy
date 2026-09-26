package dokploy

// Settle applies the operator's answers to a plan, for the copy the stages
// work from.
//
// A plan is built before anyone answers it, so its groups say a question with
// no default blocks them - and until this ran, that stayed true after the
// question was answered, and the group could never move. An item the operator
// excluded, or chose to leave on Dokploy, is not moved; its group is held when
// it shares anything with the rest, since moving half of a group splits its
// data between two platforms.
func Settle(p Plan, answers map[string]map[string]string, exclude []string) Plan {
	left := map[string]bool{}
	for _, id := range exclude {
		left[id] = true
	}
	items := make([]Item, len(p.Items))
	for i, it := range p.Items {
		for _, choice := range answers[it.ID] {
			if choice == optLeaveOnDokploy.ID {
				left[it.ID] = true
			}
		}
		if left[it.ID] && it.Verdict != NotMoved {
			it.Verdict = NotMoved
			it.Reasons = append([]string{"left on Dokploy by choice"}, it.Reasons...)
		}
		items[i] = it
	}
	byID := map[string]Item{}
	for _, it := range items {
		byID[it.ID] = it
	}

	groups := make([]Group, len(p.Groups))
	for i, g := range p.Groups {
		g.CanMove, g.Blockers = true, nil
		var stayed []string
		for _, m := range g.Members {
			if left[m.ID] {
				stayed = append(stayed, m.Name)
				continue
			}
			for _, d := range byID[m.ID].Decisions {
				if d.Default == "" && answers[m.ID][d.ID] == "" {
					g.CanMove = false
					g.Blockers = append(g.Blockers, m.Name+": "+d.Question)
				}
			}
		}
		switch {
		case len(stayed) == len(g.Members) && len(stayed) > 0:
			g.CanMove = false
			g.Blockers = append(g.Blockers, "left on Dokploy by choice")
		case len(stayed) > 0:
			g.CanMove = false
			for _, name := range stayed {
				g.Blockers = append(g.Blockers, name+" is left on Dokploy, and the rest of this group shares its data")
			}
		}
		groups[i] = g
	}
	p.Items, p.Groups = items, groups
	return p
}
