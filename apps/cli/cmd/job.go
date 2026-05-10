package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs and cron jobs",
}

var jobProject string

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		jobs, err := c.ListJobs(orgID(), pid)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			fmt.Println("No jobs found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tSCHEDULE")
		for _, j := range jobs {
			kind := "job"
			if j.IsCron {
				kind = "cron"
			}
			schedule := j.Schedule
			if schedule == "" {
				schedule = "—"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", j.ID, j.Name, kind, j.Status, schedule)
		}
		return w.Flush()
	},
}

var jobCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new job or cron job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)

		image, _ := cmd.Flags().GetString("image")
		command, _ := cmd.Flags().GetString("command")
		schedule, _ := cmd.Flags().GetString("schedule")
		concurrency, _ := cmd.Flags().GetString("concurrency")
		historyLimit, _ := cmd.Flags().GetInt("history-limit")
		isCron := schedule != ""

		if image == "" {
			return fmt.Errorf("--image is required")
		}

		body := client.CreateJobBody{
			Name:              args[0],
			IsCron:            isCron,
			Image:             image,
			Command:           command,
			Schedule:          schedule,
			ConcurrencyPolicy: concurrency,
			HistoryLimit:      historyLimit,
		}
		job, err := c.CreateJob(orgID(), pid, body)
		if err != nil {
			return err
		}
		kind := "job"
		if job.IsCron {
			kind = "cron job"
		}
		fmt.Printf("✔  %s %q created (id: %s).\n", kind, job.Name, job.ID)
		return nil
	},
}

var jobGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Get details of a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		kind := "job"
		if job.IsCron {
			kind = "cron"
		}
		fmt.Printf("ID:      %s\n", job.ID)
		fmt.Printf("Name:    %s\n", job.Name)
		fmt.Printf("Type:    %s\n", kind)
		fmt.Printf("Image:   %s\n", job.Image)
		if job.Command != "" {
			fmt.Printf("Command: %s\n", job.Command)
		}
		if job.IsCron {
			fmt.Printf("Schedule:    %s\n", job.Schedule)
			fmt.Printf("Concurrency: %s\n", job.ConcurrencyPolicy)
			fmt.Printf("History:     %d runs\n", job.HistoryLimit)
		}
		fmt.Printf("Status:  %s\n", job.Status)
		if job.LastRunAt != nil {
			fmt.Printf("Last run: %s\n", *job.LastRunAt)
		}
		return nil
	},
}

var jobUpdateCmd = &cobra.Command{
	Use:   "update <name|id>",
	Short: "Update a job's image, command, or schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		body := client.UpdateJobBody{}
		if cmd.Flags().Changed("image") {
			v, _ := cmd.Flags().GetString("image")
			body.Image = &v
		}
		if cmd.Flags().Changed("command") {
			v, _ := cmd.Flags().GetString("command")
			body.Command = &v
		}
		if cmd.Flags().Changed("schedule") {
			v, _ := cmd.Flags().GetString("schedule")
			body.Schedule = &v
		}
		if cmd.Flags().Changed("concurrency") {
			v, _ := cmd.Flags().GetString("concurrency")
			body.ConcurrencyPolicy = &v
		}
		if cmd.Flags().Changed("history-limit") {
			v, _ := cmd.Flags().GetInt("history-limit")
			body.HistoryLimit = &v
		}

		updated, err := c.UpdateJob(orgID(), pid, job.ID, body)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Job %q updated (status: %s).\n", updated.Name, updated.Status)
		return nil
	},
}

var jobRunCmd = &cobra.Command{
	Use:   "run <name|id>",
	Short: "Trigger a job run immediately",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		run, err := c.TriggerJob(orgID(), pid, job.ID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Job %q triggered: run %s (status: %s)\n", job.Name, run.ID, run.Status)
		return nil
	},
}

var jobDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete job %q? [y/N]: ", job.Name)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteJob(orgID(), pid, job.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Job %q deleted.\n", job.Name)
		return nil
	},
}

// ─── job runs subcommand ──────────────────────────────────────────────────────

var jobRunsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Manage job run history",
}

var jobRunsListCmd = &cobra.Command{
	Use:   "list <job-name|id>",
	Short: "List run history for a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		runs, err := c.ListJobRuns(orgID(), pid, job.ID)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Println("No runs found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tSTARTED\tFINISHED")
		for _, r := range runs {
			started := "—"
			if r.StartedAt != nil {
				started = *r.StartedAt
			}
			finished := "—"
			if r.FinishedAt != nil {
				finished = *r.FinishedAt
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, r.Status, started, finished)
		}
		return w.Flush()
	},
}

var jobRunsDeleteCmd = &cobra.Command{
	Use:   "delete <job-name|id> <run-id>",
	Short: "Delete a job run record",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(jobProject)
		job, err := c.GetJobByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.DeleteJobRun(orgID(), pid, job.ID, args[1]); err != nil {
			return err
		}
		fmt.Printf("✔  Run %q deleted.\n", args[1])
		return nil
	},
}

func init() {
	jobCmd.PersistentFlags().StringVarP(&jobProject, "project", "p", "", "Project ID or slug")

	jobCreateCmd.Flags().StringP("image", "i", "", "Container image (required)")
	jobCreateCmd.Flags().StringP("command", "c", "", "Command to run (overrides image CMD)")
	jobCreateCmd.Flags().String("schedule", "", "Cron schedule (e.g. '0 2 * * *') — makes it a cron job")
	jobCreateCmd.Flags().String("concurrency", "", "Concurrency policy: allow, forbid, replace")
	jobCreateCmd.Flags().Int("history-limit", 0, "Number of completed runs to retain")

	jobUpdateCmd.Flags().StringP("image", "i", "", "New container image")
	jobUpdateCmd.Flags().StringP("command", "c", "", "New command")
	jobUpdateCmd.Flags().String("schedule", "", "New cron schedule")
	jobUpdateCmd.Flags().String("concurrency", "", "Concurrency policy: allow, forbid, replace")
	jobUpdateCmd.Flags().Int("history-limit", 0, "Number of completed runs to retain")

	jobDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")

	jobRunsCmd.AddCommand(jobRunsListCmd, jobRunsDeleteCmd)
	jobCmd.AddCommand(jobListCmd, jobCreateCmd, jobGetCmd, jobUpdateCmd, jobRunCmd, jobDeleteCmd, jobRunsCmd)
	rootCmd.AddCommand(jobCmd)
}
