package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/joshkerr/goplexcli/internal/plex"
)

const (
	ServiceType = "_goplexcli._tcp"
	ServiceDomain = "local."
	DefaultPort = 8765
)

// StreamItem represents a media item available for streaming
type StreamItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Type        string    `json:"type"`
	Year        int       `json:"year,omitempty"`
	Duration    int       `json:"duration,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	StreamURL   string    `json:"stream_url"`
	PosterURL   string    `json:"poster_url,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}

// Server manages published stream items and HTTP/mDNS services
type Server struct {
	port       int
	hostname   string
	streams    map[string]*StreamItem
	streamsMu  sync.RWMutex
	httpServer *http.Server
	mdnsServer *zeroconf.Server
}

// NewServer creates a new stream server
func NewServer(port int) (*Server, error) {
	if port == 0 {
		port = DefaultPort
	}

	hostname, err := getHostname()
	if err != nil {
		hostname = "goplexcli"
	}

	return &Server{
		port:     port,
		hostname: hostname,
		streams:  make(map[string]*StreamItem),
	}, nil
}

// Start starts the HTTP and mDNS services
func (s *Server) Start(ctx context.Context) error {
	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebUI)
	mux.HandleFunc("/streams", s.handleListStreams)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Start HTTP server in background
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("http server failed: %w", err)
		}
	}()

	// Wait a moment for server to start
	time.Sleep(100 * time.Millisecond)

	// Register mDNS service
	mdnsServer, err := zeroconf.Register(
		s.hostname,      // Instance name
		ServiceType,     // Service type
		ServiceDomain,   // Domain
		s.port,          // Port
		[]string{"path=/streams"}, // TXT records
		nil,             // Network interface (nil = all)
	)
	if err != nil {
		s.httpServer.Shutdown(context.Background())
		return fmt.Errorf("failed to register mDNS service: %w", err)
	}
	s.mdnsServer = mdnsServer

	// Wait for context cancellation or error
	select {
	case err := <-errChan:
		s.mdnsServer.Shutdown()
		return err
	case <-ctx.Done():
		return s.Shutdown()
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	// Shutdown mDNS in background with timeout
	if s.mdnsServer != nil {
		done := make(chan struct{})
		go func() {
			s.mdnsServer.Shutdown()
			close(done)
		}()
		
		select {
		case <-done:
			// mDNS shutdown completed
		case <-time.After(2 * time.Second):
			// mDNS shutdown timed out, continue anyway
		}
	}
	
	// Shutdown HTTP server
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// PublishStream publishes a new stream item
func (s *Server) PublishStream(media *plex.MediaItem, streamURL string, plexURL string, plexToken string) string {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	id := generateStreamID()
	
	// Build full poster URL if thumb path exists
	posterURL := ""
	if media.Thumb != "" {
		posterURL = fmt.Sprintf("%s%s?X-Plex-Token=%s", plexURL, media.Thumb, plexToken)
	}
	
	stream := &StreamItem{
		ID:          id,
		Title:       media.FormatMediaTitle(),
		Type:        media.Type,
		Year:        media.Year,
		Duration:    media.Duration,
		Summary:     media.Summary,
		StreamURL:   streamURL,
		PosterURL:   posterURL,
		PublishedAt: time.Now(),
	}

	s.streams[id] = stream
	return id
}

// RemoveStream removes a published stream
func (s *Server) RemoveStream(id string) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	delete(s.streams, id)
}

// GetStream retrieves a stream by ID
func (s *Server) GetStream(id string) (*StreamItem, bool) {
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()
	stream, ok := s.streams[id]
	return stream, ok
}

// ListStreams returns all published streams
func (s *Server) ListStreams() []*StreamItem {
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()

	streams := make([]*StreamItem, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	return streams
}

// HTTP Handlers

func (s *Server) handleListStreams(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	streams := s.ListStreams()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"streams": streams,
		"count":   len(streams),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

// Helper functions

func getHostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return hostname, nil
}

func generateStreamID() string {
	return fmt.Sprintf("stream-%d", time.Now().UnixNano())
}

// Discovery functions

// DiscoveredServer represents a discovered goplexcli server
type DiscoveredServer struct {
	Name      string
	Host      string
	Port      int
	Addresses []string
}

// Discover finds goplexcli servers on the local network
func Discover(ctx context.Context, timeout time.Duration) ([]*DiscoveredServer, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 10)
	servers := make([]*DiscoveredServer, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Start discovery goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			mu.Lock()
			
			// Collect both IPv4 and IPv6 addresses
			addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
			for _, ip := range entry.AddrIPv4 {
				addresses = append(addresses, ip.String())
			}
			for _, ip := range entry.AddrIPv6 {
				addresses = append(addresses, ip.String())
			}
			
			server := &DiscoveredServer{
				Name:      entry.Instance,
				Host:      entry.HostName,
				Port:      entry.Port,
				Addresses: addresses,
			}
			servers = append(servers, server)
			mu.Unlock()
		}
	}()

	// Create timeout context
	discoverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Browse for services - this will close the channel when context is done
	if err := resolver.Browse(discoverCtx, ServiceType, ServiceDomain, entries); err != nil {
		// Close channel to unblock goroutine before waiting
		close(entries)
		wg.Wait()
		return nil, fmt.Errorf("failed to browse: %w", err)
	}

	// Wait for context to expire
	<-discoverCtx.Done()
	
	// Wait for goroutine to finish processing all entries
	wg.Wait()

	return servers, nil
}

// FetchStreams fetches available streams from a discovered server
func FetchStreams(server *DiscoveredServer) ([]*StreamItem, error) {
	if len(server.Addresses) == 0 {
		return nil, fmt.Errorf("no addresses available for server")
	}

	// Try each address until one works
	var lastErr error
	for _, addr := range server.Addresses {
		// Format IPv6 addresses with brackets
		host := addr
		if strings.Contains(addr, ":") {
			host = "[" + addr + "]"
		}
		url := fmt.Sprintf("http://%s:%d/streams", host, server.Port)
		
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}

		// Use anonymous function to ensure body is closed before continue
		result, err := func() ([]*StreamItem, error) {
			defer resp.Body.Close()
			
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
			}

			var result struct {
				Streams []*StreamItem `json:"streams"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return nil, err
			}

			return result.Streams, nil
		}()
		
		if err != nil {
			lastErr = err
			continue
		}
		
		// Success!
		return result, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch streams: %w", lastErr)
	}
	return nil, fmt.Errorf("no addresses responded")
}
