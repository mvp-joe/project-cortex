package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/kluctl/go-embed-python/pip"
)

func main() {
	// Command-line flag to specify platforms
	platformsFlag := flag.String("platforms", "", "Comma-separated list of platforms (e.g., darwin-arm64,linux-amd64). If empty, generates for all known platforms.")
	flag.Parse()

	// When run via go:generate, the working directory is internal/embed/server
	// So we use relative paths from there
	requirementsPath := filepath.Join("requirements.txt")
	dataPath := filepath.Join("data")

	var err error
	if *platformsFlag == "" {
		// Generate for all known platforms (default behavior)
		log.Println("Generating Python dependencies for all known platforms...")
		err = pip.CreateEmbeddedPipPackagesForKnownPlatforms(requirementsPath, dataPath)
	} else {
		// Generate for specific platforms
		platformStrs := strings.Split(*platformsFlag, ",")
		log.Printf("Generating Python dependencies for platforms: %v\n", platformStrs)

		for _, platformStr := range platformStrs {
			parts := strings.Split(strings.TrimSpace(platformStr), "-")
			if len(parts) != 2 {
				log.Fatalf("Invalid platform format: %s (expected format: os-arch, e.g., darwin-arm64)", platformStr)
			}

			goos := parts[0]
			goarch := parts[1]
			log.Printf("Generating for %s/%s...", goos, goarch)

			// CreateEmbeddedPipPackages(requirementsFile, goOs, goArch, pipPlatforms, targetDir)
			// pipPlatforms is empty/nil to let it auto-detect based on goOs/goArch
			err = pip.CreateEmbeddedPipPackages(requirementsPath, goos, goarch, nil, dataPath)
			if err != nil {
				panic(fmt.Errorf("failed to generate for %s/%s: %w", goos, goarch, err))
			}
		}
	}

	if err != nil {
		panic(err)
	}

	log.Println("Python dependencies generated successfully!")
}
