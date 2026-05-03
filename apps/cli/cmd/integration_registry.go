package cmd

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var integrationRegistryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage container registry integrations",
}

var integrationRegistryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registry integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := apiClient().ListRegistryIntegrations(orgID())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No registry integrations found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tNAMESPACE")
		for _, r := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ID, r.Name, r.Provider, r.Namespace)
		}
		return w.Flush()
	},
}

var integrationRegistryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a container registry (wizard)",
	RunE:  runRegistryAdd,
}

var integrationRegistryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a registry integration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := apiClient().DeleteRegistryIntegration(orgID(), args[0]); err != nil {
			return err
		}
		fmt.Println("✔  Registry integration deleted.")
		return nil
	},
}

type registryProviderMeta struct {
	label                string
	needsEndpoint        bool
	endpointPlaceholder  string
	userLabel            string
	passLabel            string
	namespacePlaceholder string
}

var registryProviders = []struct {
	value string
	registryProviderMeta
}{
	{"ghcr",      registryProviderMeta{label: "GitHub Container Registry", needsEndpoint: false, userLabel: "GitHub username",      passLabel: "Personal access token",   namespacePlaceholder: "ghcr.io/my-org"}},
	{"dockerhub", registryProviderMeta{label: "Docker Hub",                needsEndpoint: false, userLabel: "Docker Hub username",  passLabel: "Password or access token", namespacePlaceholder: "docker.io/my-org"}},
	{"ecr",       registryProviderMeta{label: "Amazon ECR",                needsEndpoint: true,  endpointPlaceholder: "123456789.dkr.ecr.us-east-1.amazonaws.com", userLabel: "AWS access key ID", passLabel: "AWS secret access key", namespacePlaceholder: "123456789.dkr.ecr.us-east-1.amazonaws.com"}},
	{"gcr",       registryProviderMeta{label: "Google Container Registry", needsEndpoint: true,  endpointPlaceholder: "gcr.io",     userLabel: "Username (_json_key)",     passLabel: "Service account JSON",     namespacePlaceholder: "gcr.io/my-project"}},
	{"custom",    registryProviderMeta{label: "Private Registry",          needsEndpoint: true,  endpointPlaceholder: "registry.example.com", userLabel: "Username", passLabel: "Password or token", namespacePlaceholder: "registry.example.com/my-org"}},
}

func runRegistryAdd(_ *cobra.Command, _ []string) error {
	sc := bufio.NewScanner(os.Stdin)
	fmt.Println()

	// Provider
	providerLabels := make([]string, len(registryProviders))
	for i, p := range registryProviders {
		providerLabels[i] = p.label
	}
	idx := pickOneIdx(sc, "Provider", providerLabels)
	p := registryProviders[idx]

	// Label
	name := prompt(sc, "Label (e.g. production-registry)")
	if name == "" {
		return fmt.Errorf("label is required")
	}

	// Endpoint (conditional)
	var endpoint string
	if p.needsEndpoint {
		endpoint = prompt(sc, fmt.Sprintf("Endpoint (%s)", p.endpointPlaceholder))
		if endpoint == "" {
			return fmt.Errorf("endpoint is required for %s", p.label)
		}
	}

	// Namespace (optional)
	namespace := promptDefault(sc, fmt.Sprintf("Namespace (optional, e.g. %s)", p.namespacePlaceholder), "")

	// Username
	username := prompt(sc, p.userLabel)
	if username == "" {
		return fmt.Errorf("%s is required", p.userLabel)
	}

	// Password (hidden)
	fmt.Printf("%s: ", p.passLabel)
	passBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	password := string(passBytes)
	if password == "" {
		return fmt.Errorf("%s is required", p.passLabel)
	}

	reg, err := apiClient().CreateRegistryIntegration(orgID(), client.CreateRegistryBody{
		Name:      name,
		Provider:  p.value,
		Endpoint:  endpoint,
		Namespace: namespace,
		Username:  username,
		Password:  password,
	})
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}

	fmt.Printf("\n✔  Registry %q added (ID: %s)\n", reg.Name, reg.ID)
	return nil
}

func init() {
	integrationRegistryCmd.AddCommand(integrationRegistryListCmd, integrationRegistryAddCmd, integrationRegistryDeleteCmd)
}
