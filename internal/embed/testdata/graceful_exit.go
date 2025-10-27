// +build ignore

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// graceful_exit is a test helper that exits cleanly on SIGTERM
func main() {
	// Create buffered channel and register signal handler before anything else
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// Write a byte to stdout to signal readiness
	fmt.Print("READY\n")

	select {
	case <-sigChan:
		// Gracefully exit on SIGTERM
		os.Exit(0)
	case <-time.After(100 * time.Second):
		// Timeout
		os.Exit(1)
	}
}
