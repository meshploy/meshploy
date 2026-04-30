package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage project secrets",
}

var secretProject string

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(secretProject)
		secrets, err := c.ListSecrets(orgID(), pid)
		if err != nil {
			return err
		}
		if len(secrets) == 0 {
			fmt.Println("No secrets found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tCREATED")
		for _, s := range secrets {
			fmt.Fprintf(w, "%s\t%s\t%s\n", s.ID, s.Name, s.CreatedAt)
		}
		return w.Flush()
	},
}

var secretSetCmd = &cobra.Command{
	Use:   "set <name> [value]",
	Short: "Create or overwrite a secret",
	Long: `Set a secret value. If value is omitted, it is read from stdin.
Use a pipe for scripting:
  echo "mysecret" | meshploy secret set MY_KEY`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		var value string

		if len(args) == 2 {
			value = args[1]
		} else {
			// Check if stdin is a pipe/redirect.
			if !term.IsTerminal(int(syscall.Stdin)) {
				scanner := bufio.NewScanner(os.Stdin)
				scanner.Scan()
				value = strings.TrimRight(scanner.Text(), "\r\n")
			} else {
				fmt.Printf("Value for %s: ", name)
				b, err := term.ReadPassword(int(syscall.Stdin))
				fmt.Println()
				if err != nil {
					return err
				}
				value = string(b)
			}
		}

		c := apiClient()
		pid := resolveProjectID(secretProject)
		s, err := c.SetSecret(orgID(), pid, name, value)
		if err != nil {
			return err
		}
		fmt.Printf("✔  Secret %q saved (%s).\n", s.Name, s.ID)
		return nil
	},
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a secret",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete secret %q? [y/N]: ", args[0])
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		c := apiClient()
		pid := resolveProjectID(secretProject)
		return c.DeleteSecret(orgID(), pid, args[0])
	},
}

func init() {
	secretCmd.PersistentFlags().StringVarP(&secretProject, "project", "p", "", "Project ID or slug")
	secretDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	secretCmd.AddCommand(secretListCmd, secretSetCmd, secretDeleteCmd)
	rootCmd.AddCommand(secretCmd)
}
