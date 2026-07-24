package cmd

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"text/tabwriter"

	"github.com/meshploy/apps/cli/client"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var integrationStorageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage object storage integrations",
}

var integrationStorageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List storage integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := apiClient().ListStorageIntegrations(orgID())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No storage integrations found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tBUCKET\tREGION")
		for _, s := range items {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Provider, s.Bucket, s.Region)
		}
		return w.Flush()
	},
}

var integrationStorageAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add an object storage integration (wizard)",
	RunE:  runStorageAdd,
}

var integrationStorageDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a storage integration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := apiClient().DeleteStorageIntegration(orgID(), args[0]); err != nil {
			return err
		}
		fmt.Println("✔  Storage integration deleted.")
		return nil
	},
}

type storageProviderMeta struct {
	label               string
	needsEndpoint       bool
	endpointPlaceholder string
	needsRegion         bool
}

var storageProviders = []struct {
	value string
	storageProviderMeta
}{
	{"s3",    storageProviderMeta{label: "Amazon S3",     needsEndpoint: false, needsRegion: true}},
	{"r2",    storageProviderMeta{label: "Cloudflare R2", needsEndpoint: true,  endpointPlaceholder: "https://<account-id>.r2.cloudflarestorage.com"}},
	{"minio", storageProviderMeta{label: "MinIO",         needsEndpoint: true,  endpointPlaceholder: "https://minio.example.com"}},
	{"b2",    storageProviderMeta{label: "Backblaze B2",  needsEndpoint: true,  endpointPlaceholder: "https://s3.us-west-004.backblazeb2.com"}},
}

func runStorageAdd(_ *cobra.Command, _ []string) error {
	sc := bufio.NewScanner(os.Stdin)
	fmt.Println()

	// Provider
	providerLabels := make([]string, len(storageProviders))
	for i, p := range storageProviders {
		providerLabels[i] = p.label
	}
	idx := pickOneIdx(sc, "Provider", providerLabels)
	p := storageProviders[idx]

	// Label
	name := prompt(sc, "Label (e.g. production-backups)")
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

	// Region (S3 only)
	var region string
	if p.needsRegion {
		region = promptDefault(sc, "Region", "us-east-1")
	}

	// Bucket
	bucket := prompt(sc, "Bucket name")
	if bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	// Access key ID
	accessKeyID := prompt(sc, "Access key ID")
	if accessKeyID == "" {
		return fmt.Errorf("access key ID is required")
	}

	// Secret access key (hidden)
	fmt.Print("Secret access key: ")
	secretBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read secret: %w", err)
	}
	secretKey := string(secretBytes)
	if secretKey == "" {
		return fmt.Errorf("secret access key is required")
	}

	sto, err := apiClient().CreateStorageIntegration(orgID(), client.CreateStorageBody{
		Name:            name,
		Provider:        p.value,
		Endpoint:        endpoint,
		Region:          region,
		Bucket:          bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretKey,
	})
	if err != nil {
		return fmt.Errorf("create storage: %w", err)
	}

	fmt.Printf("\n✔  Storage %q added (ID: %s)\n", sto.Name, sto.ID)
	return nil
}

func init() {
	integrationStorageCmd.AddCommand(integrationStorageListCmd, integrationStorageAddCmd, integrationStorageDeleteCmd)
}
