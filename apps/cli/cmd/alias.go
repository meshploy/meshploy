package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage shell aliases for the meshploy binary",
}

var aliasInstallCmd = &cobra.Command{
	Use:   "install [name]",
	Short: "Create a short alias symlink for the meshploy binary",
	Long: `Creates a symlink in the same directory as the meshploy binary.

Defaults to 'mploy' if no name is given.

Examples:
  meshploy alias install        # creates mploy
  meshploy alias install mp     # creates mp`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := "mploy"
		if len(args) == 1 {
			name = args[0]
		}

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}

		dir := filepath.Dir(exePath)
		linkPath := filepath.Join(dir, name)

		// Remove existing symlink if present.
		if info, err := os.Lstat(linkPath); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("%s already exists and is not a symlink — remove it manually first", linkPath)
			}
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("remove existing symlink: %w", err)
			}
		}

		if err := os.Symlink(exePath, linkPath); err != nil {
			return fmt.Errorf("create symlink (may need sudo): %w", err)
		}

		fmt.Printf("✔  Created %s → %s\n", linkPath, exePath)
		return nil
	},
}

var aliasRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Remove a meshploy alias symlink",
	Long: `Removes a symlink alias for the meshploy binary.

If a name is given, removes that specific alias.
If no name is given, removes all symlinks in the same directory that point to this binary.

Examples:
  meshploy alias remove        # removes all aliases pointing to meshploy
  meshploy alias remove mp     # removes only 'mp'`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		dir := filepath.Dir(exePath)

		// Specific name given — remove just that one.
		if len(args) == 1 {
			return removeAlias(filepath.Join(dir, args[0]))
		}

		// No name — scan directory for symlinks pointing to this binary.
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read directory: %w", err)
		}
		found := false
		for _, e := range entries {
			if e.Name() == filepath.Base(exePath) {
				continue
			}
			fullPath := filepath.Join(dir, e.Name())
			info, err := os.Lstat(fullPath)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(fullPath)
			if err != nil || target != exePath {
				continue
			}
			if err := removeAlias(fullPath); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
			found = true
		}
		if !found {
			fmt.Println("No aliases found pointing to this binary.")
		}
		return nil
	},
}

func removeAlias(linkPath string) error {
	info, err := os.Lstat(linkPath)
	if os.IsNotExist(err) {
		fmt.Printf("%s does not exist.\n", linkPath)
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a symlink — remove it manually", linkPath)
	}
	if err := os.Remove(linkPath); err != nil {
		return fmt.Errorf("remove symlink (may need sudo): %w", err)
	}
	fmt.Printf("✔  Removed %s\n", linkPath)
	return nil
}

func init() {
	aliasCmd.AddCommand(aliasInstallCmd, aliasRemoveCmd)
	rootCmd.AddCommand(aliasCmd)
}
