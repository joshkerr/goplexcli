//go:build windows

package main

import "os"

// fullRescanSignal has no equivalent on Windows; the channel never fires.
func fullRescanSignal() <-chan os.Signal { return nil }
