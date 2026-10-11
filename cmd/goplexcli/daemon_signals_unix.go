//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// fullRescanSignal delivers SIGUSR1, which asks the daemon for a full
// re-enrichment scan (like 'library scan --full').
func fullRescanSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	return ch
}
