package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/meshploy/apps/cli/client"
	"github.com/spf13/cobra"
)

// ── Manifest types ────────────────────────────────────────────────────────────

type ServiceManifest struct {
	Service ServiceBlock `toml:"service"`
	Build   *BuildBlock  `toml:"build,omitempty"`
	DB      *DBBlock     `toml:"database,omitempty"`
	Env     map[string]string `toml:"env,omitempty"`
}

type ServiceBlock struct {
	Name     string `toml:"name"`
	Type     string `toml:"type"`
	Port     int    `toml:"port,omitempty"`
	Replicas int    `toml:"replicas,omitempty"`
}

type BuildBlock struct {
	Source         string        `toml:"source"`
	IntegrationID  string        `toml:"integration_id,omitempty"`
	RepoURL        string        `toml:"repo_url,omitempty"`
	Branch         string        `toml:"branch,omitempty"`
	Builder        string        `toml:"builder,omitempty"`
	DockerfilePath string        `toml:"dockerfile_path,omitempty"`
	Image          string        `toml:"image,omitempty"`
	Registry       *RegistryRef  `toml:"registry,omitempty"`
}

type RegistryRef struct {
	IntegrationID string `toml:"integration_id"`
	Image         string `toml:"image"`
}

type DBBlock struct {
	Engine    string `toml:"engine"`
	Version   string `toml:"version"`
	StorageGB int    `toml:"storage_gb"`
}

// ── Wizard ────────────────────────────────────────────────────────────────────

var serviceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Interactively create a service and save a meshploy.toml manifest",
	RunE:  runServiceCreate,
}

func runServiceCreate(cmd *cobra.Command, args []string) error {
	sc := bufio.NewScanner(os.Stdin)
	c := apiClient()
	org := orgID()

	fmt.Println("Meshploy service wizard — answers in [brackets] are defaults, press Enter to accept.")
	fmt.Println()

	// ── Name ──────────────────────────────────────────────────────────────────
	name := prompt(sc, "Service name")
	if name == "" {
		return fmt.Errorf("service name is required")
	}

	// ── Type ──────────────────────────────────────────────────────────────────
	svcType := pickOne(sc, "Service type", []string{"application", "database"})

	manifest := ServiceManifest{
		Service: ServiceBlock{Name: name, Type: svcType},
	}

	switch svcType {
	case "application":
		if err := wizardApplication(sc, c, org, &manifest); err != nil {
			return err
		}
	case "database":
		if err := wizardDatabase(sc, &manifest); err != nil {
			return err
		}
	}

	// ── Save ──────────────────────────────────────────────────────────────────
	outPath := name + ".meshploy.toml"

	// Warn if not inside a git repo.
	if _, err := os.Stat(".git"); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		fmt.Printf("\n⚠  No git repository detected in %s\n", cwd)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	abs, _ := filepath.Abs(outPath)
	fmt.Printf("\n✔  Saved to %s\n", abs)
	fmt.Printf("   Run `meshploy service deploy %s` to deploy.\n", name)
	return nil
}

// ── Application wizard ────────────────────────────────────────────────────────

func wizardApplication(sc *bufio.Scanner, c *client.Client, org string, m *ServiceManifest) error {
	m.Service.Port = promptInt(sc, "Port", 3000)
	m.Service.Replicas = promptInt(sc, "Replicas", 1)

	build := &BuildBlock{}

	// Build source
	build.Source = pickOne(sc, "Build source", []string{"git", "image"})

	if build.Source == "git" {
		if err := wizardGitSource(sc, c, org, build); err != nil {
			return err
		}
	} else {
		build.Image = prompt(sc, "Image (e.g. nginx:latest)")
	}

	// Registry (optional for git builds, required for image builds)
	regs, err := c.ListRegistryIntegrations(org)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  (could not load registries: %v)\n", err)
	}
	if len(regs) > 0 {
		options := make([]string, len(regs)+1)
		options[0] = "— skip —"
		for i, r := range regs {
			options[i+1] = fmt.Sprintf("%s (%s)", r.Name, r.Provider)
		}
		idx := pickOneIdx(sc, "Container registry", options)
		if idx > 0 {
			reg := regs[idx-1]
			imageName := prompt(sc, fmt.Sprintf("Image name (e.g. %s/myapp)", reg.Namespace))
			build.Registry = &RegistryRef{
				IntegrationID: reg.ID,
				Image:         imageName,
			}
		}
	} else if build.Source == "git" {
		fmt.Println("  ℹ  No registry integrations found — add one in the Meshploy UI to push built images.")
	}

	m.Build = build

	// Env vars
	m.Env = wizardEnv(sc)
	return nil
}

func wizardGitSource(sc *bufio.Scanner, c *client.Client, org string, build *BuildBlock) error {
	integrations, err := c.ListGitIntegrations(org)
	if err != nil {
		return fmt.Errorf("load git integrations: %w", err)
	}

	connected := make([]client.GitIntegration, 0, len(integrations))
	for _, g := range integrations {
		if g.Connected {
			connected = append(connected, g)
		}
	}

	if len(connected) == 0 {
		fmt.Println("  ✖  No connected git integrations found. Add one in the Meshploy UI first.")
		build.RepoURL = prompt(sc, "Repository URL (manual)")
		build.Branch = promptDefault(sc, "Branch", "main")
	} else {
		labels := make([]string, len(connected))
		for i, g := range connected {
			labels[i] = fmt.Sprintf("%s (%s)", g.Name, g.Provider)
		}
		idx := pickOneIdx(sc, "Git integration", labels)
		git := connected[idx]
		build.IntegrationID = git.ID

		// Try to list repos; fall back to manual entry on error.
		repos, err := c.ListRepos(org, git.ID)
		if err != nil || len(repos) == 0 {
			build.RepoURL = prompt(sc, "Repository URL")
		} else {
			repoLabels := make([]string, len(repos))
			for i, r := range repos {
				repoLabels[i] = r.FullName
			}
			ridx := pickOneIdx(sc, "Repository", repoLabels)
			build.RepoURL = "https://github.com/" + repos[ridx].FullName

			// Branches
			branches, err := c.ListBranches(org, git.ID, build.RepoURL)
			if err != nil || len(branches) == 0 {
				build.Branch = promptDefault(sc, "Branch", repos[ridx].DefaultBranch)
			} else {
				build.Branch = pickOne(sc, "Branch", branches)
			}
		}

		if build.Branch == "" {
			build.Branch = promptDefault(sc, "Branch", "main")
		}
	}

	// Builder
	build.Builder = pickOne(sc, "Builder", []string{"nixpacks", "dockerfile", "buildpack"})
	if build.Builder == "dockerfile" {
		build.DockerfilePath = promptDefault(sc, "Dockerfile path", "./Dockerfile")
	}
	return nil
}

// ── Database wizard ───────────────────────────────────────────────────────────

func wizardDatabase(sc *bufio.Scanner, m *ServiceManifest) error {
	engine := pickOne(sc, "Database engine", []string{"postgres", "mysql", "redis", "mongodb"})

	versionMap := map[string]string{
		"postgres": "16",
		"mysql":    "8",
		"redis":    "7",
		"mongodb":  "7",
	}
	defaultPort := map[string]int{
		"postgres": 5432,
		"mysql":    3306,
		"redis":    6379,
		"mongodb":  27017,
	}

	version := promptDefault(sc, "Version", versionMap[engine])
	m.Service.Port = defaultPort[engine]

	storageGB := promptInt(sc, "Storage (GB)", 10)

	m.DB = &DBBlock{
		Engine:    engine,
		Version:   version,
		StorageGB: storageGB,
	}
	return nil
}

// ── Env vars ──────────────────────────────────────────────────────────────────

func wizardEnv(sc *bufio.Scanner) map[string]string {
	fmt.Print("\nEnvironment variables (KEY=VALUE, empty line to finish):\n")
	env := map[string]string{}
	for {
		fmt.Print("  > ")
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			break
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			fmt.Println("  ✖  Format must be KEY=VALUE")
			continue
		}
		env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// ── Prompt helpers ────────────────────────────────────────────────────────────

func prompt(sc *bufio.Scanner, label string) string {
	fmt.Printf("%s: ", label)
	sc.Scan()
	return strings.TrimSpace(sc.Text())
}

func promptDefault(sc *bufio.Scanner, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	sc.Scan()
	v := strings.TrimSpace(sc.Text())
	if v == "" {
		return def
	}
	return v
}

func promptInt(sc *bufio.Scanner, label string, def int) int {
	fmt.Printf("%s [%d]: ", label, def)
	sc.Scan()
	v := strings.TrimSpace(sc.Text())
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		fmt.Printf("  ✖  Invalid number, using %d\n", def)
		return def
	}
	return n
}

// pickOne shows a numbered menu and returns the selected string value.
func pickOne(sc *bufio.Scanner, label string, options []string) string {
	return options[pickOneIdx(sc, label, options)]
}

// pickOneIdx shows a numbered menu and returns the zero-based index.
func pickOneIdx(sc *bufio.Scanner, label string, options []string) int {
	fmt.Printf("%s:\n", label)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	for {
		fmt.Print("> ")
		sc.Scan()
		v := strings.TrimSpace(sc.Text())
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("  ✖  Enter a number between 1 and %d\n", len(options))
			continue
		}
		return n - 1
	}
}

func init() {
	serviceCmd.AddCommand(serviceCreateCmd)
}
