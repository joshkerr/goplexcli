//go:build windows

package progress

import (
	"net"
	"os"
	"time"
)

// pipeConn wraps an os.File to implement net.Conn for Windows named pipes.
type pipeConn struct {
	*os.File
}

func (p *pipeConn) LocalAddr() net.Addr                { return pipeAddr{p.Name()} }
func (p *pipeConn) RemoteAddr() net.Addr               { return pipeAddr{p.Name()} }
func (p *pipeConn) SetDeadline(t time.Time) error      { return nil }
func (p *pipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (p *pipeConn) SetWriteDeadline(t time.Time) error { return nil }

type pipeAddr struct{ name string }

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return a.name }

// dialMPV connects to MPV's IPC using named pipes on Windows.
// Named pipes can be opened as files on Windows.
func dialMPV(pipePath string) (net.Conn, error) {
	// Open the named pipe as a file for read/write
	file, err := os.OpenFile(pipePath, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &pipeConn{file}, nil
}
