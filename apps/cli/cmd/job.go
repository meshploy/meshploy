package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

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

func init() {
	jobCmd.PersistentFlags().StringVarP(&jobProject, "project", "p", "", "Project ID or slug")
	jobDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	jobCmd.AddCommand(jobListCmd, jobRunCmd, jobDeleteCmd)
	rootCmd.AddCommand(jobCmd)
}
