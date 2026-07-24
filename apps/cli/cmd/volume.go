package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/client"
	"github.com/spf13/cobra"
)

var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "Manage persistent volumes",
}

var volumeProject string

var volumeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List volumes in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)
		volumes, err := c.ListVolumes(orgID(), pid)
		if err != nil {
			return err
		}
		if len(volumes) == 0 {
			fmt.Println("No volumes found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSLUG\tSIZE (GB)\tSTATUS")
		for _, v := range volumes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", v.ID, v.Name, v.Slug, v.StorageGB, v.Status)
		}
		return w.Flush()
	},
}

var volumeCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new volume",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)
		sizeStr, _ := cmd.Flags().GetString("size")
		size, err := strconv.Atoi(sizeStr)
		if err != nil || size <= 0 {
			return fmt.Errorf("invalid size %q — must be a positive integer (GB)", sizeStr)
		}
		vol, err := c.CreateVolume(orgID(), pid, client.CreateVolumeBody{
			Name:      args[0],
			StorageGB: size,
		})
		if err != nil {
			return err
		}
		fmt.Printf("✔  Volume %q created (id: %s, slug: %s, %d GB).\n", vol.Name, vol.ID, vol.Slug, vol.StorageGB)
		return nil
	},
}

var volumeGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Get details of a volume",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)
		vol, err := c.GetVolumeByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		fmt.Printf("ID:      %s\n", vol.ID)
		fmt.Printf("Name:    %s\n", vol.Name)
		fmt.Printf("Slug:    %s\n", vol.Slug)
		fmt.Printf("Size:    %d GB\n", vol.StorageGB)
		fmt.Printf("Status:  %s\n", vol.Status)
		return nil
	},
}

var volumeAttachCmd = &cobra.Command{
	Use:   "attach <volume> --service <name|id> --mount <path>",
	Short: "Attach a volume to a service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)

		serviceRef, _ := cmd.Flags().GetString("service")
		mountPath, _ := cmd.Flags().GetString("mount")
		if serviceRef == "" || mountPath == "" {
			return fmt.Errorf("--service and --mount are required")
		}

		vol, err := c.GetVolumeByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		svc, err := c.GetServiceByName(orgID(), pid, serviceRef)
		if err != nil {
			return err
		}
		mount, err := c.AttachVolume(orgID(), pid, vol.ID, client.AttachVolumeBody{
			ServiceID: svc.ID,
			MountPath: mountPath,
		})
		if err != nil {
			return err
		}
		fmt.Printf("✔  Volume %q attached to service %q at %s (mount id: %s).\n",
			vol.Name, svc.Name, mount.MountPath, mount.ID)
		return nil
	},
}

var volumeDetachCmd = &cobra.Command{
	Use:   "detach <volume> --mount <mount-id>",
	Short: "Detach a volume mount",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)

		mountID, _ := cmd.Flags().GetString("mount")
		if mountID == "" {
			return fmt.Errorf("--mount <mount-id> is required")
		}

		vol, err := c.GetVolumeByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		if err := c.DetachVolume(orgID(), pid, vol.ID, mountID); err != nil {
			return err
		}
		fmt.Printf("✔  Mount %q detached from volume %q.\n", mountID, vol.Name)
		return nil
	},
}

var volumeDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a volume",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		pid := resolveProjectID(volumeProject)
		vol, err := c.GetVolumeByName(orgID(), pid, args[0])
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete volume %q? This is irreversible. [y/N]: ", vol.Name)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		if err := c.DeleteVolume(orgID(), pid, vol.ID); err != nil {
			return err
		}
		fmt.Printf("✔  Volume %q deleted.\n", vol.Name)
		return nil
	},
}

func init() {
	volumeCmd.PersistentFlags().StringVarP(&volumeProject, "project", "p", "", "Project ID or slug")
	volumeCreateCmd.Flags().StringP("size", "s", "5", "Size in GB")
	volumeAttachCmd.Flags().String("service", "", "Service name or ID")
	volumeAttachCmd.Flags().String("mount", "", "Mount path inside the container")
	volumeDetachCmd.Flags().String("mount", "", "Mount ID to detach")
	volumeDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	volumeCmd.AddCommand(volumeListCmd, volumeCreateCmd, volumeGetCmd, volumeAttachCmd, volumeDetachCmd, volumeDeleteCmd)
	rootCmd.AddCommand(volumeCmd)
}
