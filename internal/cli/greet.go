package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	greetName string
)

// greetCmd represents an example subcommand
var greetCmd = &cobra.Command{
	Use:   "greet",
	Short: "Greet someone",
	Long:  `An example command that demonstrates how to create subcommands with flags.`,
	Run: func(cmd *cobra.Command, args []string) {
		name := greetName
		if name == "" && len(args) > 0 {
			name = args[0]
		}
		if name == "" {
			name = "World"
		}
		fmt.Printf("Hello, %s!\n", name)
	},
}

func init() {
	rootCmd.AddCommand(greetCmd)
	greetCmd.Flags().StringVarP(&greetName, "name", "n", "", "Name to greet")
}
