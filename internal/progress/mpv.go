// Package progress provides playback progress tracking for media players.
// It includes an IPC client for communicating with MPV media player to track
// playback position and state, which is then used to report progress to Plex.
//
// The IPC connection uses TCP sockets on localhost for cross-platform compatibility
// (works on Windows, macOS, and Linux).
package progress

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Connection retry settings for MPV IPC socket
const (
	maxConnectRetries = 50                     // Maximum number of connection attempts
	connectRetryDelay = 100 * time.Millisecond // Delay between connection attempts
)

// mpvCommand represents a command to send to MPV via JSON IPC.
type mpvCommand struct {
	Command []interface{} `json:"command"`
}

// mpvResponse represents a response from MPV's JSON IPC.
type mpvResponse struct {
	Data  interface{} `json:"data"`
	Error string      `json:"error"`
}

// buildMPVCommand creates an mpvCommand with the given command and arguments.
func buildMPVCommand(cmd string, args ...string) mpvCommand {
	command := make([]interface{}, 0, 1+len(args))
	command = append(command, cmd)
	for _, arg := range args {
		command = append(command, arg)
	}
	return mpvCommand{Command: command}
}

// MPVClient provides communication with MPV via its JSON IPC protocol.
// It connects to MPV over a TCP socket on localhost and can query playback state.
// TCP is used instead of Unix sockets for cross-platform compatibility (Windows/macOS/Linux).
type MPVClient struct {
	address string
	conn    net.Conn
	reader  *bufio.Reader
	mu      sync.Mutex
}

// NewMPVClient creates a new MPV IPC client for the given address.
// The address should be in the format "127.0.0.1:PORT".
// The client is not connected until Connect is called.
func NewMPVClient(address string) *MPVClient {
	return &MPVClient{
		address: address,
	}
}

// GenerateIPCAddress creates a unique IPC address for MPV communication.
// Returns a TCP address on localhost with a semi-random port based on PID.
// The port is in the range 19000-19999 to avoid conflicts with common services.
func GenerateIPCAddress() string {
	// Use PID to generate a semi-unique port in range 19000-19999
	port := 19000 + (time.Now().UnixNano() % 1000)
	return fmt.Sprintf("127.0.0.1:%d", port)
}

// Connect establishes a connection to the MPV IPC server.
// It retries with a short delay to allow MPV time to start the IPC server.
func (c *MPVClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	// Retry connection with backoff since MPV may take time to start IPC server
	var lastErr error
	for i := 0; i < maxConnectRetries; i++ {
		conn, err := net.Dial("tcp", c.address)
		if err == nil {
			c.conn = conn
			c.reader = bufio.NewReader(conn)
			return nil
		}
		lastErr = err
		time.Sleep(connectRetryDelay)
	}

	return fmt.Errorf("failed to connect to MPV IPC server after retries: %w", lastErr)
}

// Close closes the connection to MPV.
func (c *MPVClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	return err
}

// IsConnected returns true if the client has an active connection.
func (c *MPVClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// sendCommand sends a command to MPV and returns the response.
func (c *MPVClient) sendCommand(cmd mpvCommand) (*mpvResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected to MPV")
	}

	// Marshal the command to JSON
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %w", err)
	}

	// Send the command with newline terminator
	data = append(data, '\n')
	if _, err := c.conn.Write(data); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	// Read the response
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the response
	var resp mpvResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for MPV errors
	if resp.Error != "success" {
		return &resp, fmt.Errorf("MPV error: %s", resp.Error)
	}

	return &resp, nil
}

// GetTimePos returns the current playback position in seconds.
func (c *MPVClient) GetTimePos() (float64, error) {
	cmd := buildMPVCommand("get_property", "time-pos")
	resp, err := c.sendCommand(cmd)
	if err != nil {
		return 0, err
	}

	// MPV returns the time position as a float64
	pos, ok := resp.Data.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected time-pos type: %T", resp.Data)
	}

	return pos, nil
}

// GetPaused returns true if playback is paused.
func (c *MPVClient) GetPaused() (bool, error) {
	cmd := buildMPVCommand("get_property", "pause")
	resp, err := c.sendCommand(cmd)
	if err != nil {
		return false, err
	}

	// MPV returns pause state as a bool
	paused, ok := resp.Data.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected pause type: %T", resp.Data)
	}

	return paused, nil
}

// GetPlaylistPos returns the current playlist position (0-indexed).
func (c *MPVClient) GetPlaylistPos() (int, error) {
	cmd := buildMPVCommand("get_property", "playlist-pos")
	resp, err := c.sendCommand(cmd)
	if err != nil {
		return 0, err
	}

	// MPV returns playlist position as a float64 (JSON numbers)
	pos, ok := resp.Data.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected playlist-pos type: %T", resp.Data)
	}

	return int(pos), nil
}

// GetDuration returns the total duration of the current media in seconds.
func (c *MPVClient) GetDuration() (float64, error) {
	cmd := buildMPVCommand("get_property", "duration")
	resp, err := c.sendCommand(cmd)
	if err != nil {
		return 0, err
	}

	// MPV returns duration as a float64
	duration, ok := resp.Data.(float64)
	if !ok {
		return 0, fmt.Errorf("unexpected duration type: %T", resp.Data)
	}

	return duration, nil
}

// GetFilename returns the filename of the currently playing media.
func (c *MPVClient) GetFilename() (string, error) {
	cmd := buildMPVCommand("get_property", "filename")
	resp, err := c.sendCommand(cmd)
	if err != nil {
		return "", err
	}

	// MPV returns filename as a string
	filename, ok := resp.Data.(string)
	if !ok {
		return "", fmt.Errorf("unexpected filename type: %T", resp.Data)
	}

	return filename, nil
}

// GetPlaybackState returns the current playback state information.
// This is a convenience method that combines multiple property queries.
type PlaybackState struct {
	TimePos     float64
	Duration    float64
	Paused      bool
	PlaylistPos int
}

// GetPlaybackState returns the current playback state.
func (c *MPVClient) GetPlaybackState() (*PlaybackState, error) {
	state := &PlaybackState{}

	// Get time position
	timePos, err := c.GetTimePos()
	if err != nil {
		return nil, fmt.Errorf("failed to get time position: %w", err)
	}
	state.TimePos = timePos

	// Get duration
	duration, err := c.GetDuration()
	if err != nil {
		// Duration might not be available yet, use 0
		state.Duration = 0
	} else {
		state.Duration = duration
	}

	// Get pause state
	paused, err := c.GetPaused()
	if err != nil {
		return nil, fmt.Errorf("failed to get pause state: %w", err)
	}
	state.Paused = paused

	// Get playlist position
	playlistPos, err := c.GetPlaylistPos()
	if err != nil {
		// Playlist position might not be relevant, use 0
		state.PlaylistPos = 0
	} else {
		state.PlaylistPos = playlistPos
	}

	return state, nil
}
