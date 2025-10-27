package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var (
	// Version information - typically set via ldflags at build time
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

// getVersion returns the version, trying ldflags first, then debug.BuildInfo
func getVersion() string {
	if Version != "dev" {
		return Version
	}

	// Fallback: get version from module info (works with go install)
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}

	return "dev"
}

// getGitCommit returns the git commit, trying ldflags first, then debug.BuildInfo
func getGitCommit() string {
	if GitCommit != "none" {
		return GitCommit
	}

	// Fallback: get commit from build settings
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				if len(setting.Value) > 7 {
					return setting.Value[:7] // Short hash like git
				}
				return setting.Value
			}
		}
	}

	return "none"
}

// getBuildDate returns the build date, trying ldflags first, then debug.BuildInfo
func getBuildDate() string {
	if BuildDate != "unknown" {
		return BuildDate
	}

	// Fallback: get build time from build settings
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				return setting.Value
			}
		}
	}

	return "unknown"
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of Cortex",
	Long:  `All software has versions. This is Cortex's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Cortex %s\n", getVersion())
		fmt.Printf("Git commit: %s\n", getGitCommit())
		fmt.Printf("Build date: %s\n", getBuildDate())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
