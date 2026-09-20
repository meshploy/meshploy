package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Plan moving a server from another platform into Meshploy",
	Long: `Plan moving a server that runs another platform into Meshploy.

Everything here only reads the server: nothing in the other platform, Docker
or the host is changed.`,
}

var migrateDokployCmd = &cobra.Command{
	Use:   "dokploy",
	Short: "Plan moving a Dokploy server",
	Long: `Plan moving a server that runs Dokploy.

  detect   whether this is a Dokploy server, which version, what holds ports 80
           and 443, and what the server has to spare
  plan     what each project, application, compose app, database, domain and
           integration becomes in Meshploy, and what needs you
  prepare  stage 1: create the Meshploy side of a confirmed plan, with services
           stopped and routes paused. Dokploy keeps serving
  move     stage 2: move one group - stop Dokploy's copies, start Meshploy's,
           switch that group's domains
  cutover  stage 3: hand ports 80 and 443 to Meshploy, with the certificates
  rollback undo one group, or everything, back to Dokploy

Run it on the Dokploy server, as root: it reads Docker and Dokploy's database.
Secrets are read only in memory and never printed or written.`,
}

var migrateDokployDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect Dokploy on this server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy detect requires root: it reads Docker; re-run with sudo")
		}
		src, err := dokploy.Collect(migrate.ExecRunner{})
		if err != nil && !src.Detection.Dokploy {
			return err
		}
		plan := dokploy.BuildPlan(dokploy.Source{Detection: src.Detection, Docker: src.Docker, Listeners: src.Listeners, Resources: src.Resources, DynamicFiles: src.DynamicFiles, AcmeBytes: src.AcmeBytes}, time.Now())
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"detection": plan.Detection, "edge": plan.Edge, "resources": plan.Resources, "mode": plan.Mode})
		}
		printDetection(cmd.OutOrStdout(), plan)
		return nil
	},
}

var migrateDokployPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Plan moving this Dokploy server into Meshploy",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy plan requires root: it reads Docker and Dokploy's database; re-run with sudo")
		}
		src, err := dokploy.Collect(migrate.ExecRunner{})
		if err != nil {
			return err
		}
		if !src.Detection.Dokploy {
			return fmt.Errorf("Dokploy was not found on this server")
		}
		if !src.Detection.Supported {
			return fmt.Errorf("cannot plan: %s", src.Detection.SupportNote)
		}
		plan := dokploy.BuildPlan(src, time.Now())

		if out, _ := cmd.Flags().GetString("out"); out != "" {
			f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := writeJSON(f, plan); err != nil {
				return err
			}
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(cmd.OutOrStdout(), plan)
		}
		printPlan(cmd.OutOrStdout(), plan)
		return nil
	},
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printDetection(w io.Writer, p dokploy.Plan) {
	d := p.Detection
	if !d.Dokploy {
		fmt.Fprintln(w, "Dokploy:    not found on this server")
		return
	}
	version := d.Version
	if version == "" {
		version = "unknown version"
	}
	fmt.Fprintf(w, "Dokploy:    %s (%s), schema level %d\n", version, orDash(d.ImageTag), d.Migrations)
	if d.Supported {
		fmt.Fprintln(w, "Supported:  yes")
	} else {
		fmt.Fprintf(w, "Supported:  no, %s\n", d.SupportNote)
	}
	fmt.Fprintf(w, "Edge:       %s", p.Edge.Kind)
	if len(p.Edge.Holders) > 0 {
		fmt.Fprintf(w, " (ports 80/443 held by %s)", strings.Join(p.Edge.Holders, ", "))
	}
	fmt.Fprintf(w, ", %d Traefik route files\n", p.Edge.DynamicFiles)
	if p.Edge.Note != "" {
		fmt.Fprintf(w, "            %s\n", p.Edge.Note)
	}
	r := p.Resources
	fmt.Fprintf(w, "Resources:  %d cores, %d of %d MB memory free (workloads use %d MB), %d MB disk free, %d MB in Docker volumes\n",
		r.Cores, r.AvailableMB, r.MemoryMB, r.WorkloadMemoryMB, r.DiskFreeMB, r.DockerVolumeMB)
	fmt.Fprintf(w, "Mode:       %s: %s\n", p.Mode.Choice, p.Mode.Reason)
}

func printPlan(w io.Writer, p dokploy.Plan) {
	printDetection(w, p)
	fmt.Fprintf(w, "\n%d move, %d need you, %d are not moved; %d decisions have no default\n",
		p.Summary[dokploy.Moves], p.Summary[dokploy.NeedsYou], p.Summary[dokploy.NotMoved], p.Summary["open_decisions"])

	if len(p.Groups) > 0 {
		movable := 0
		for _, g := range p.Groups {
			if g.CanMove {
				movable++
			}
		}
		fmt.Fprintf(w, "\nGroups (%d, %d can move now): each moves together, in this order\n", len(p.Groups), movable)
		for i, g := range p.Groups {
			mark := "✔"
			if !g.CanMove {
				mark = "!"
			}
			fmt.Fprintf(w, "  %d. %s %s\n", i+1, mark, g.Name)
			var names []string
			for _, m := range g.Members {
				names = append(names, m.Kind+" "+m.Name)
			}
			fmt.Fprintf(w, "       members:  %s\n", strings.Join(names, ", "))
			if len(g.Data) > 0 {
				var data []string
				for _, d := range g.Data {
					data = append(data, fmt.Sprintf("%s %d MB (%s)", d.Name, d.MB, d.Move))
				}
				fmt.Fprintf(w, "       data:     %s\n", strings.Join(data, "; "))
			}
			fmt.Fprintf(w, "       downtime: %s\n", g.Downtime)
			for _, b := range g.Blockers {
				fmt.Fprintf(w, "       blocked:  %s\n", b)
			}
			for _, n := range g.Notes {
				fmt.Fprintf(w, "       note:     %s\n", n)
			}
		}
	}

	order := []string{"project", "application", "compose", "database", "domain", "git_provider", "registry", "destination", "backup", "certificate", "server"}
	titles := map[string]string{"project": "Projects", "application": "Applications", "compose": "Compose apps", "database": "Databases",
		"domain": "Domains", "git_provider": "Git providers", "registry": "Registries", "destination": "Backup storage",
		"backup": "Backup schedules", "certificate": "Certificates", "server": "Remote servers"}
	marks := map[string]string{dokploy.Moves: "✔", dokploy.NeedsYou: "!", dokploy.NotMoved: "✗"}

	byKind := map[string][]dokploy.Item{}
	for _, it := range p.Items {
		byKind[it.Kind] = append(byKind[it.Kind], it)
	}
	for _, kind := range order {
		items := byKind[kind]
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Project+items[i].Name < items[j].Project+items[j].Name })
		fmt.Fprintf(w, "\n%s (%d)\n", titles[kind], len(items))
		for _, it := range items {
			name := it.Name
			if it.Project != "" {
				name = it.Project + " / " + it.Name
			}
			state := ""
			if it.Running != nil && !*it.Running {
				state = " [stopped]"
			}
			fmt.Fprintf(w, "  %s %s%s → %s\n", marks[it.Verdict], name, state, it.MapsTo)
			if mb := it.Details["data_mb"]; mb != "" {
				fmt.Fprintf(w, "      data: %s MB, moved by %s\n", mb, it.Details["data_move"])
			}
			for _, reason := range it.Reasons {
				if strings.HasSuffix(reason, "with the database stopped") {
					continue // said by the data line
				}
				fmt.Fprintf(w, "      %s\n", reason)
			}
			for _, d := range it.Decisions {
				choice := "no default: you choose"
				for _, o := range d.Options {
					if o.ID == d.Default {
						choice = "default: " + o.Label
					}
				}
				fmt.Fprintf(w, "      ? %s\n        %s\n", d.Question, choice)
			}
		}
	}

	if len(p.Unmanaged) > 0 {
		fmt.Fprintf(w, "\nNot managed by Dokploy (%d)\n", len(p.Unmanaged))
		for _, u := range p.Unmanaged {
			fmt.Fprintf(w, "  ? %s (%s, %d of %d running) → %s\n", u.Name, u.Kind, u.Running, u.Total, u.MapsTo)
			if u.Note != "" {
				fmt.Fprintf(w, "      %s\n", u.Note)
			}
		}
	}
	fmt.Fprintln(w, "\n✔ moves   ! needs you   ✗ not moved   ? offered for import")
}

// migrateDokployFixtureCmd writes a scrubbed reading of this server, for the
// golden plans in the repository.
//
// Every name, host, path and secret is replaced before anything is written, so
// the file can be committed and read by anyone; what survives is the structure
// a plan is built from. It is the honest way to test the reader against real
// servers without carrying anyone's data around.
var migrateDokployPrepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Stage 1: build the Meshploy side of a confirmed plan, with nothing serving",
	Long: `Creates projects, workloads and routes in Meshploy from the confirmed plan.

Everything is created inert: services stopped, routes paused. Dokploy keeps
serving, nothing of its is stopped or changed, and no data is copied - that is
stage 2, one group at a time.

Safe to run again: each step is journalled, so an interrupted run carries on
rather than creating a second copy.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy prepare requires root: it reads Dokploy's database and the migration's credential; re-run with sudo")
		}
		body, err := runMigratePrepare()
		if err != nil {
			return err
		}
		var result dokploy.PrepareResult
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %d, skipped %d (already done).\n", result.Created, result.Skipped)
		fmt.Fprintf(cmd.OutOrStdout(), "Nothing is serving yet: services are stopped and routes are paused.\n")
		return nil
	},
}

var migrateDokployMoveCmd = &cobra.Command{
	Use:   "move <group>",
	Short: "Stage 2: move one group into Meshploy",
	Long: `Moves one group: stops Dokploy's copies, starts Meshploy's, switches the
group's domains through the edge Dokploy still runs, and checks they answer.

If any step fails the group puts itself back - Dokploy serving again, Meshploy's
copies stopped, the domains restored - because a half-moved group must never
serve.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy move requires root: it stops Docker workloads and rewrites the edge; re-run with sudo")
		}
		body, err := runMigrateMove(args[0])
		if err != nil {
			return err
		}
		var result dokploy.MoveResult
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s moved. Its domains now answer from Meshploy; downtime was %s.\n", args[0], result.Downtime)
		return nil
	},
}

var migrateDokployCutoverCmd = &cobra.Command{
	Use:   "cutover",
	Short: "Stage 3: hand ports 80 and 443 to Meshploy",
	Long: `Backs up the old edge and its certificates, imports the certificates for moved
domains so HTTPS works immediately, stops the old edge and starts Meshploy's.

Seconds of refused connections between the two. Every group must have moved
first: cutover takes the ports for every domain at once.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy cutover requires root: it stops the edge and starts another; re-run with sudo")
		}
		body, err := runMigrateCutover()
		if err != nil {
			return err
		}
		var result dokploy.CutoverResult
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Meshploy holds 80 and 443. %d certificate(s) carried over; the ports changed hands in %s.\n",
			len(result.CertificatesImported), result.Downtime)
		return nil
	},
}

var migrateDokployRollbackCmd = &cobra.Command{
	Use:   "rollback [group]",
	Short: "Undo one group, or the whole migration, back to Dokploy",
	Long: `Replays the journal backwards: routes paused, domains given back, Meshploy's
copies stopped, Dokploy's started again.

Data written into Meshploy after a group moved is not copied back.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy rollback requires root; re-run with sudo")
		}
		var group string
		if len(args) == 1 {
			group = args[0]
		}
		// Data is the one thing a rollback cannot undo. Dokploy's copy is
		// untouched - it was only ever read - but anything written into
		// Meshploy since the copy stays there, and going back means going back
		// to the data as it was at the stop.
		copied, err := movedData(group)
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if len(copied) > 0 && !yes {
			fmt.Fprintln(cmd.OutOrStdout(), "This migration has already copied data:")
			for _, c := range copied {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s, %s\n", c.Name, c.At.Local().Format("2 Jan 15:04"))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Dokploy's copy is as it was at the stop. Anything written into Meshploy since then stays in Meshploy.")
			fmt.Fprint(cmd.OutOrStdout(), "Roll back anyway? [y/N]: ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}
		body, err := runMigrateRollback(group)
		if err != nil {
			return err
		}
		var result dokploy.Result
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Undid %d step(s).\n", result.Undone)
		return nil
	},
}

var migrateDokployFixtureCmd = &cobra.Command{
	Use:   "fixture",
	Short: "Write a scrubbed reading of this server, for use as a test fixture",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("migrate dokploy fixture requires root: it reads Docker and Dokploy's database; re-run with sudo")
		}
		src, err := dokploy.Collect(migrate.ExecRunner{})
		if err != nil {
			return err
		}
		if !src.Detection.Dokploy {
			return fmt.Errorf("Dokploy was not found on this server")
		}
		scrubbed := dokploy.Scrub(src)

		out, _ := cmd.Flags().GetString("out")
		if out == "" {
			return writeJSON(cmd.OutOrStdout(), scrubbed)
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := writeJSON(f, scrubbed); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", out)
		return nil
	},
}

func init() {
	migrateDokployDetectCmd.Flags().Bool("json", false, "Print the result as JSON")
	migrateDokployFixtureCmd.Flags().String("out", "", "Write the scrubbed reading to this file (created readable by root only)")
	migrateDokployPlanCmd.Flags().Bool("json", false, "Print the plan as JSON")
	migrateDokployRollbackCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation about data already copied")
	migrateDokployPlanCmd.Flags().String("out", "", "Also write the plan as JSON to this file (created readable by root only)")
	migrateDokployCmd.AddCommand(migrateDokployDetectCmd, migrateDokployPlanCmd, migrateDokployPrepareCmd,
		migrateDokployMoveCmd, migrateDokployCutoverCmd, migrateDokployRollbackCmd, migrateDokployFixtureCmd)
	migrateCmd.AddCommand(migrateDokployCmd)
	rootCmd.AddCommand(migrateCmd)
}
