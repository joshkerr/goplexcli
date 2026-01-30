//go:build !windows

package progress

import "net"

// dialMPV connects to MPV's IPC socket using Unix domain sockets.
func dialMPV(socketPath string) (net.Conn, error) {
	return net.Dial("unix", socketPath)
}
