package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/meshploy/packages/client"
	"github.com/spf13/cobra"
)

var (
	deployFolderService string
	deployFolderProject string
	deployFolderPort    int
	deployFolderNoWait  bool
	deployFolderOnly    bool
)

// deployCmd builds and runs a folder on this machine: packed, uploaded as the
// service's source, and built on the cluster the way a repository is.
var deployCmd = &cobra.Command{
	Use:   "deploy <folder>",
	Short: "Deploy a folder from this machine",
	Long: `Packs the folder, uploads it as a service's source and deploys it.

Left out: .git, node_modules and other installed dependencies, .env files
(an .env.example is kept), and whatever the folder's .gitignore and
.meshployignore name. The service is the folder's name unless --service names
another; one that does not exist yet is created.`,
	Example: `  meshploy deploy .
  meshploy deploy ./my-app --project tools --port 8080`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		folder, err := client.PackFolder(args[0])
		if err != nil {
			return err
		}
		c := apiClient()
		pid := resolveProjectID(deployFolderProject)

		name := deployFolderService
		if name == "" {
			name = serviceNameFor(folder.Name)
		}
		svc, err := c.GetServiceByName(orgID(), pid, name)
		if err != nil {
			svc, err = c.CreateService(orgID(), pid, client.CreateServiceBody{
				Name:       name,
				FromUpload: true,
				Ports:      []client.ServicePortBody{{Name: "http", Port: deployFolderPort, IsHTTP: true, IsPrimary: true, IsPublic: true}},
			})
			if err != nil {
				return fmt.Errorf("create service %q: %w", name, err)
			}
			fmt.Printf("✔  Created service %q\n", svc.Name)
		}

		fmt.Printf("↑  Uploading %s: %d files, %s", folder.Name, folder.Files, humanBytes(len(folder.Archive)))
		if len(folder.Skipped) > 0 {
			fmt.Printf(" (left out %s)", strings.Join(firstN(folder.Skipped, 4), ", "))
		}
		fmt.Println()
		res, err := c.UploadSource(orgID(), pid, svc.ID, folder, !deployFolderOnly)
		if err != nil {
			return err
		}
		if res.Deployment == nil {
			fmt.Printf("✔  Uploaded %s. Deploy it with: meshploy service deploy %s\n", short(res.Source.Digest), svc.Name)
			return nil
		}
		fmt.Printf("✔  Uploaded %s, deploying (%s)\n", short(res.Source.Digest), res.Deployment.ID)
		if deployFolderNoWait {
			return nil
		}
		return followDeployment(c, pid, svc.ID, res.Deployment.ID)
	},
}

// followDeployment prints each status a deployment passes through, and
// returns an error when it fails.
func followDeployment(c *client.Client, pid, serviceID, deploymentID string) error {
	last := ""
	for {
		d, err := c.GetDeployment(orgID(), pid, serviceID, deploymentID)
		if err != nil {
			return err
		}
		if d.Status != last {
			fmt.Printf("   %s\n", d.Status)
			last = d.Status
		}
		switch d.Status {
		case "success":
			fmt.Println("✔  Deployed")
			return nil
		case "failed", "cancelled":
			return errors.New("the deployment " + d.Status + ": see its log with meshploy service deployments, or in the console")
		}
		time.Sleep(3 * time.Second)
	}
}

var notNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// serviceNameFor makes a folder's name a service name: lower case, letters,
// digits and dashes.
func serviceNameFor(folder string) string {
	n := strings.Trim(notNameChars.ReplaceAllString(strings.ToLower(folder), "-"), "-")
	if n == "" {
		return "app"
	}
	return n
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func short(digest string) string {
	d := strings.TrimPrefix(digest, "sha256:")
	if len(d) > 12 {
		d = d[:12]
	}
	return d
}

func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(append([]string{}, list[:n]...), fmt.Sprintf("%d more", len(list)-n))
}

func init() {
	deployCmd.Flags().StringVar(&deployFolderService, "service", "", "Service to deploy to (default: the folder's name)")
	deployCmd.Flags().StringVarP(&deployFolderProject, "project", "p", "", "Project slug or ID")
	deployCmd.Flags().IntVar(&deployFolderPort, "port", 3000, "Port the app listens on, for a service created now")
	deployCmd.Flags().BoolVar(&deployFolderNoWait, "no-wait", false, "Return once the deploy has started")
	deployCmd.Flags().BoolVar(&deployFolderOnly, "upload-only", false, "Upload without deploying")
	rootCmd.AddCommand(deployCmd)
}
