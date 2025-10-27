// +build ignore

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ignore_sigterm is a test helper that ignores SIGTERM and only exits on SIGKILL
func main() {
	// Create buffered channel and register signal handler
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// Drain SIGTERM signals but don't exit
	go func() {
		for range sigChan {
			// Ignore SIGTERM
		}
	}()

	// Write a byte to stdout to signal readiness
	fmt.Print("READY\n")

	// Sleep forever (until killed)
	time.Sleep(100 * time.Second)
	os.Exit(1)
}
