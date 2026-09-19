package dockerapi

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// Collect reads every container on the host into the report the API serves.
//
// Stats are optional and separate: they cost about a second per container, so
// the inventory refreshes often and the numbers refresh rarely. Passing false
// keeps whatever the caller already had.
func Collect(ctx context.Context, withStats bool) hostagent.Docker {
	out := hostagent.Docker{CheckedAt: time.Now().UTC()}

	client, err := Open(ctx)
	if err != nil {
		// A host with no runtime is not a broken host: the console shows no
		// tab rather than an error.
		out.Error = err.Error()
		return out
	}
	if v, err := client.Version(ctx); err == nil {
		out.Runtime, out.Version = v.Runtime(), v.Version
	}

	summaries, err := client.Containers(ctx)
	if err != nil {
		out.Error = "list containers: " + err.Error()
		return out
	}

	for _, s := range summaries {
		c := hostagent.Container{
			ID:          s.ID,
			Name:        s.Name(),
			Image:       s.Image,
			State:       s.State,
			Status:      s.Status,
			NetworkMode: s.HostConfig.NetworkMode,
			Project:     s.Labels["com.docker.compose.project"],
			Service:     s.Labels["com.docker.compose.service"],
			Swarm:       s.Labels["com.docker.swarm.service.name"],
		}
		for _, p := range s.Ports {
			if p.PublicPort == 0 {
				continue // exposed, not published: nothing on the host to reach
			}
			c.Ports = append(c.Ports, hostagent.Port{
				HostIP: p.IP, HostPort: p.PublicPort, Port: p.PrivatePort, Protocol: p.Type,
			})
		}
		for _, m := range s.Mounts {
			switch m.Type {
			case "bind":
				c.BindSources = append(c.BindSources, m.Source)
			case "volume":
				if m.Name != "" {
					c.Volumes = append(c.Volumes, m.Name)
				}
			}
		}

		if d, err := client.Inspect(ctx, s.ID); err == nil {
			c.RestartCount = d.RestartCount
			c.CreatedAt = parseTime(d.Created)
			c.StartedAt = parseTime(d.State.StartedAt)
			if d.State.Health != nil {
				c.Health = strings.ToLower(d.State.Health.Status)
			}
		}

		if withStats && s.State == "running" {
			if st, err := client.StatsOnce(ctx, s.ID); err == nil {
				c.MemoryMB, c.CPUPercent = st.MemoryMB(), round1(st.CPUPercent())
			}
		}
		out.Containers = append(out.Containers, c)
	}

	sort.Slice(out.Containers, func(i, j int) bool { return out.Containers[i].Name < out.Containers[j].Name })
	if withStats {
		out.StatsAt = out.CheckedAt
	}
	return out
}

// parseTime reads the Engine's timestamps, which are RFC3339 with nanoseconds,
// and returns the zero time for the one it uses to mean "never started".
func parseTime(s string) time.Time {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// CarryStats keeps the numbers from the previous report on a refresh that did
// not read them, so the console shows a figure with an age rather than a gap.
func CarryStats(fresh hostagent.Docker, previous *hostagent.Docker) hostagent.Docker {
	if previous == nil || previous.StatsAt.IsZero() {
		return fresh
	}
	was := map[string]hostagent.Container{}
	for _, c := range previous.Containers {
		was[c.ID] = c
	}
	for i, c := range fresh.Containers {
		old, ok := was[c.ID]
		if !ok || c.MemoryMB != 0 || c.CPUPercent != 0 {
			continue
		}
		// Only for a container that has been running since: a restart makes
		// the old numbers somebody else's.
		if !old.StartedAt.Equal(c.StartedAt) {
			continue
		}
		fresh.Containers[i].MemoryMB, fresh.Containers[i].CPUPercent = old.MemoryMB, old.CPUPercent
	}
	fresh.StatsAt = previous.StatsAt
	return fresh
}
