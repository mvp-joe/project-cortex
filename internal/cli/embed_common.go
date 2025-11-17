package cli

import (
	"github.com/spf13/cobra"
)

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Embedding server commands",
	Long:  `Manage the embedding daemon server.`,
}

func init() {
	rootCmd.AddCommand(embedCmd)
}

