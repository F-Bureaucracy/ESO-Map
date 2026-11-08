// Package cli implements the eso-map command line interface.
package cli

import (
	"github.com/spf13/cobra"
)

// Build information, overridden at link time via -ldflags "-X ...".
var (
	version = "dev"
	commit  = "none"
)

// NewRootCommand returns the eso-map root command.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "eso-map",
		Short: "Map External Secrets Operator configuration end to end",
		Long: `eso-map scans a directory of Kubernetes YAML manifests and maps how secrets
flow through the External Secrets Operator:

  provider (OpenBao, Vault, AWS, ...) path
    → SecretStore / ClusterSecretStore / Generator
    → ExternalSecret / ClusterExternalSecret / PushSecret
    → Kubernetes Secret key
    → workload env var, envFrom, volume mount, imagePullSecret or Ingress TLS

The result can be rendered as JSON, YAML or an interactive HTML diagram.`,
		Version:       version + " (" + commit + ")",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScanCommand())
	return root
}
