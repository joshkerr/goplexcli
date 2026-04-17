//go:build android

// Package termuxfix works around a bug in Termux's packaged golang 1.26.2
// (github.com/termux/termux-packages #29385): the runtime's goargs() skips
// argv[1] when GOOS=android && CGO_ENABLED=0, so os.Args is missing the
// first real argument (and is empty when the program is invoked with none).
// Reading /proc/self/cmdline gives us the actual kernel-provided argv; we
// rebuild os.Args from it. Must be imported as the first blank import in
// main so its init runs before pflag's package-level CommandLine = NewFlagSet(os.Args[0], ...).
package termuxfix

import (
	"os"
	"strings"
)

func init() {
	data, err := os.ReadFile("/proc/self/cmdline")
	if err != nil || len(data) == 0 {
		return
	}
	if data[len(data)-1] == 0 {
		data = data[:len(data)-1]
	}
	os.Args = strings.Split(string(data), "\x00")
}
