package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage services and databases",
}

var serviceProject string

var serviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List services in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		services, err := c.ListServices(orgID(), pid)
		if err != nil {
			return err
		}
		if len(services) == 0 {
			fmt.Println("No services found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tIMAGE")
		for _, s := range services {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Type, s.Status, s.Image)
		}
		return w.Flush()
	},
}

var serviceDeployCmd = &cobra.Command{
	Use:   "deploy <name|id>",
	Short: "Trigger a new deployment for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		d, err := c.Deploy(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Deployment triggered: %s (status: %s)\n", d.ID, d.Status)
		return nil
	},
}

var serviceStartCmd = &cobra.Command{
	Use:   "start <name|id>",
	Short: "Start a stopped service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.StartService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q starting.\n", svc.Name)
		return nil
	},
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop <name|id>",
	Short: "Stop a running service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.StopService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q stopped.\n", svc.Name)
		return nil
	},
}

var (
	logsTail   int
	logsSince  string
	logsFollow bool
)

var serviceLogsCmd = &cobra.Command{
	Use:   "logs <name|id>",
	Short: "Stream live container logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if logsFollow {
			fmt.Fprintf(os.Stderr, "Streaming logs for %q — press Ctrl+C to stop\n", svc.Name)
		}
		return c.StreamLogs(orgID(), pid, svc.ID, logsTail, logsSince, logsFollow, os.Stdout)
	},
}

var serviceDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete service %q? [y/N]: ", svc.Name)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteService(orgID(), pid, svc.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Service %q deleted.\n", svc.Name)
		return nil
	},
}

var serviceDeploymentsCmd = &cobra.Command{
	Use:   "deployments <name|id>",
	Short: "List deployment history for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		deployments, err := c.ListDeployments(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		if len(deployments) == 0 {
			fmt.Println("No deployments found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tIMAGE\tCREATED")
		for _, d := range deployments {
			image := d.Image
			if len(image) > 40 {
				image = image[:37] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.ID, d.Status, image, d.CreatedAt)
		}
		return w.Flush()
	},
}

var serviceRollbackCmd = &cobra.Command{
	Use:   "rollback <name|id>",
	Short: "Roll back a service to the previous successful deployment",
	Long: `Roll back to the previous successful deployment.
Use --to <deployment-id> to target a specific deployment.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		targetID, _ := cmd.Flags().GetString("to")
		if targetID == "" {
			// Pick the most recent successful deployment that isn't the current one.
			deployments, err := c.ListDeployments(orgID(), pid, svc.ID)
			if err != nil {
				return err
			}
			skippedFirst := false
			for _, d := range deployments {
				if d.Status == "success" {
					if !skippedFirst {
						skippedFirst = true
						continue
					}
					targetID = d.ID
					break
				}
			}
			if targetID == "" {
				return fmt.Errorf("no previous successful deployment found to roll back to")
			}
		}

		d, err := c.RollbackDeployment(orgID(), pid, svc.ID, targetID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Rollback triggered: %s (status: %s)\n", d.ID, d.Status)
		return nil
	},
}

var serviceCancelCmd = &cobra.Command{
	Use:   "cancel <name|id>",
	Short: "Cancel the active deployment for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		deployments, err := c.ListDeployments(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		for _, d := range deployments {
			if d.Status == "pending" || d.Status == "building" || d.Status == "deploying" {
				if err := c.CancelDeployment(orgID(), pid, svc.ID, d.ID); err != nil {
					return err
				}
				fmt.Printf("✔  Deployment %s cancelled.\n", d.ID)
				return nil
			}
		}
		return fmt.Errorf("no active deployment found for service %q", svc.Name)
	},
}

var serviceRetryCmd = &cobra.Command{
	Use:   "retry <name|id>",
	Short: "Retry the latest failed deployment",
	Long: `Retry the most recent failed deployment.
Pass a deployment ID as a second argument to retry a specific one.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		var targetID string
		if len(args) == 2 {
			targetID = args[1]
		} else {
			deployments, err := c.ListDeployments(orgID(), pid, svc.ID)
			if err != nil {
				return err
			}
			for _, d := range deployments {
				if d.Status == "failed" {
					targetID = d.ID
					break
				}
			}
			if targetID == "" {
				return fmt.Errorf("no failed deployment found for service %q", svc.Name)
			}
		}

		d, err := c.RetryDeployment(orgID(), pid, svc.ID, targetID)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Retry triggered: %s (status: %s)\n", d.ID, d.Status)
		return nil
	},
}

// ─── service env ──────────────────────────────────────────────────────────────

var serviceEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage runtime environment variables",
}

var serviceEnvGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Print the decrypted .env block for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		envVars, err := c.GetEnvVars(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		fmt.Print(envVars)
		if envVars != "" && !strings.HasSuffix(envVars, "\n") {
			fmt.Println()
		}
		return nil
	},
}

var serviceEnvSetCmd = &cobra.Command{
	Use:   "set <name|id> [KEY=VALUE ...]",
	Short: "Set runtime environment variables (merges with existing)",
	Long: `Set runtime environment variables for a service.

With KEY=VALUE arguments, the given keys are merged into the existing .env block.
With --file, the entire .env block is replaced with the file contents.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		envFile, _ := cmd.Flags().GetString("file")
		var final string

		if envFile != "" {
			b, err := os.ReadFile(envFile)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			final = string(b)
		} else {
			if len(args) < 2 {
				return fmt.Errorf("provide KEY=VALUE arguments or --file .env")
			}
			existing, err := c.GetEnvVars(orgID(), pid, svc.ID)
			if err != nil {
				return err
			}
			final = mergeEnvVars(existing, args[1:])
		}

		if err := c.SetEnvVars(orgID(), pid, svc.ID, final); err != nil {
			return err
		}
		fmt.Printf("✔  Environment variables updated for %q.\n", svc.Name)
		return nil
	},
}

// ─── service build-config ─────────────────────────────────────────────────────

var serviceBuildConfigCmd = &cobra.Command{
	Use:   "build-config",
	Short: "Manage the build configuration for a service",
}

var serviceBuildConfigGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Print the build configuration for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		bc, err := c.GetBuildConfig(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		printBuildConfig(bc)
		return nil
	},
}

var serviceBuildConfigSetCmd = &cobra.Command{
	Use:   "set <name|id>",
	Short: "Update the build configuration for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		body := client.UpdateBuildConfigBody{}
		if cmd.Flags().Changed("repo") {
			v, _ := cmd.Flags().GetString("repo")
			body.GitRepo = &v
		}
		if cmd.Flags().Changed("branch") {
			v, _ := cmd.Flags().GetString("branch")
			body.Branch = &v
		}
		if cmd.Flags().Changed("builder") {
			v, _ := cmd.Flags().GetString("builder")
			body.Builder = &v
		}
		if cmd.Flags().Changed("dockerfile") {
			v, _ := cmd.Flags().GetString("dockerfile")
			body.DockerfilePath = &v
		}
		if cmd.Flags().Changed("auto-deploy") {
			v, _ := cmd.Flags().GetBool("auto-deploy")
			body.AutoDeploy = &v
		}
		if cmd.Flags().Changed("git-integration") {
			v, _ := cmd.Flags().GetString("git-integration")
			body.GitIntegrationID = &v
		}
		if cmd.Flags().Changed("registry") {
			v, _ := cmd.Flags().GetString("registry")
			body.RegistryIntegrationID = &v
		}

		bc, err := c.UpdateBuildConfig(orgID(), pid, svc.ID, body)
		if err != nil {
			return err
		}
		printBuildConfig(bc)
		return nil
	},
}

// ─── service build-env ────────────────────────────────────────────────────────

var serviceBuildEnvCmd = &cobra.Command{
	Use:   "build-env",
	Short: "Manage build-time environment variables",
}

var serviceBuildEnvGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Print the build-time .env block for a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		envVars, err := c.GetBuildEnvVars(orgID(), pid, svc.ID)
		if err != nil {
			return err
		}
		fmt.Print(envVars)
		if envVars != "" && !strings.HasSuffix(envVars, "\n") {
			fmt.Println()
		}
		return nil
	},
}

var serviceBuildEnvSetCmd = &cobra.Command{
	Use:   "set <name|id> [KEY=VALUE ...]",
	Short: "Set build-time environment variables (merges with existing)",
	Long: `Set build-time environment variables for a service.

With KEY=VALUE arguments, the given keys are merged into the existing .env block.
With --file, the entire build .env block is replaced with the file contents.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(serviceProject)
		svc, err := c.GetServiceByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}

		envFile, _ := cmd.Flags().GetString("file")
		var final string

		if envFile != "" {
			b, err := os.ReadFile(envFile)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			final = string(b)
		} else {
			if len(args) < 2 {
				return fmt.Errorf("provide KEY=VALUE arguments or --file .env")
			}
			existing, err := c.GetBuildEnvVars(orgID(), pid, svc.ID)
			if err != nil {
				return err
			}
			final = mergeEnvVars(existing, args[1:])
		}

		if err := c.SetBuildEnvVars(orgID(), pid, svc.ID, final); err != nil {
			return err
		}
		fmt.Printf("✔  Build environment variables updated for %q.\n", svc.Name)
		return nil
	},
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// mergeEnvVars overlays KEY=VALUE pairs onto an existing .env block.
// Existing keys are overwritten; new keys are appended.
func mergeEnvVars(existing string, pairs []string) string {
	env := map[string]string{}
	var order []string
	for _, line := range strings.Split(existing, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			if _, seen := env[k]; !seen {
				order = append(order, k)
			}
			env[k] = v
		}
	}
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: skipping %q — not in KEY=VALUE format\n", pair)
			continue
		}
		if _, seen := env[k]; !seen {
			order = append(order, k)
		}
		env[k] = v
	}
	var sb strings.Builder
	for _, k := range order {
		sb.WriteString(k + "=" + env[k] + "\n")
	}
	return sb.String()
}

func printBuildConfig(bc *client.BuildConfig) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Builder:\t%s\n", bc.Builder)
	fmt.Fprintf(w, "Git repo:\t%s\n", bc.GitRepo)
	fmt.Fprintf(w, "Branch:\t%s\n", bc.Branch)
	fmt.Fprintf(w, "Dockerfile:\t%s\n", bc.DockerfilePath)
	fmt.Fprintf(w, "Auto-deploy:\t%v\n", bc.AutoDeploy)
	if bc.LastBuiltImage != "" {
		fmt.Fprintf(w, "Last built:\t%s\n", bc.LastBuiltImage)
	}
	w.Flush()
}

func init() {
	serviceCmd.PersistentFlags().StringVarP(&serviceProject, "project", "p", "", "Project ID or slug")

	serviceDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	serviceLogsCmd.Flags().IntVar(&logsTail, "tail", 0, "Number of recent lines to show (0 = server default of 200)")
	serviceLogsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since duration: 1h, 6h, 24h, 7d")
	serviceLogsCmd.Flags().BoolVar(&logsFollow, "follow", true, "Stream new log lines (set --follow=false to fetch a snapshot)")
	serviceRollbackCmd.Flags().String("to", "", "Specific deployment ID to roll back to")

	serviceEnvSetCmd.Flags().StringP("file", "f", "", "Replace the entire .env block from a file")
	serviceEnvCmd.AddCommand(serviceEnvGetCmd, serviceEnvSetCmd)

	serviceBuildConfigSetCmd.Flags().String("repo", "", "Git repository URL")
	serviceBuildConfigSetCmd.Flags().String("branch", "", "Git branch")
	serviceBuildConfigSetCmd.Flags().String("builder", "", "Builder type: nixpacks, railpack, dockerfile, image")
	serviceBuildConfigSetCmd.Flags().String("dockerfile", "", "Path to Dockerfile (dockerfile builder only)")
	serviceBuildConfigSetCmd.Flags().Bool("auto-deploy", false, "Enable auto-deploy on push")
	serviceBuildConfigSetCmd.Flags().String("git-integration", "", "Git integration ID")
	serviceBuildConfigSetCmd.Flags().String("registry", "", "Registry integration ID (empty string = internal registry)")
	serviceBuildConfigCmd.AddCommand(serviceBuildConfigGetCmd, serviceBuildConfigSetCmd)

	serviceBuildEnvSetCmd.Flags().StringP("file", "f", "", "Replace the entire build .env block from a file")
	serviceBuildEnvCmd.AddCommand(serviceBuildEnvGetCmd, serviceBuildEnvSetCmd)

	serviceCmd.AddCommand(
		serviceListCmd, serviceDeployCmd, serviceStartCmd, serviceStopCmd,
		serviceLogsCmd, serviceDeleteCmd,
		serviceDeploymentsCmd, serviceRollbackCmd, serviceCancelCmd, serviceRetryCmd,
		serviceEnvCmd, serviceBuildConfigCmd, serviceBuildEnvCmd,
	)
	rootCmd.AddCommand(serviceCmd)
}

// resolveProjectID resolves a project from the flag or linked .meshploy file,
// then looks it up by slug if needed.
func resolveProjectID(flagVal string) string {
	ref := resolveProject(flagVal)
	// If it looks like a UUID, use directly.
	if isUUID(ref) {
		return ref
	}
	// Otherwise resolve slug → ID.
	c := apiClient()
	proj, err := c.GetProjectBySlugOrID(orgID(), ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return proj.ID
}

func isUUID(s string) bool {
	return len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
