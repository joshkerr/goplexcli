//go:build windows

package progress

import (
	"fmt"
	"net"
)

// dialMPV on Windows returns an error as named pipe support requires additional libraries.
// Progress tracking is not yet supported on Windows.
// TODO: Add Windows named pipe support using github.com/Microsoft/go-winio
func dialMPV(pipePath string) (net.Conn, error) {
	return nil, fmt.Errorf("progress tracking not yet supported on Windows")
}
