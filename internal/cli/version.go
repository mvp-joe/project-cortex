package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Version information - typically set via ldflags at build time
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of Cortex",
	Long:  `All software has versions. This is Cortex's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Cortex %s\n", Version)
		fmt.Printf("Git commit: %s\n", GitCommit)
		fmt.Printf("Build date: %s\n", BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
