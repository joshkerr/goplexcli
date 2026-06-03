// Package webdav discovers gowebdav servers advertised on the local network
// via mDNS and exposes them as transfer targets. gowebdav registers itself
// under the "_gowebdav._tcp" service type (see github.com/joshkerr/gowebdav),
// so no central registry is needed: mDNS multicast on the LAN is the registry.
package webdav

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	// ServiceType is the mDNS service type gowebdav registers under.
	ServiceType = "_gowebdav._tcp"
	// ServiceDomain is the mDNS domain used for discovery.
	ServiceDomain = "local."
)

// Target represents a gowebdav server discovered on the local network.
type Target struct {
	Name      string   // mDNS instance name (gowebdav -name flag, or hostname)
	Host      string   // resolved hostname (e.g. "office.local.")
	Port      int      // server port
	Addresses []string // IPv4 + IPv6 addresses
	Scheme    string   // "http" or "https" (from the scheme= TXT record)
}

// BaseURL returns the WebDAV base URL for the target, preferring the first
// IPv4 address. IPv6 addresses are wrapped in brackets. Returns an empty string
// if the target has no addresses.
func (t *Target) BaseURL() string {
	if len(t.Addresses) == 0 {
		return ""
	}
	addr := t.Addresses[0]
	if strings.Contains(addr, ":") {
		addr = "[" + addr + "]"
	}
	scheme := t.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, addr, t.Port)
}

// Discover finds gowebdav servers on the local network, blocking until the
// timeout elapses. It mirrors the discovery approach used for goplexcli's own
// stream servers (internal/stream.Discover).
func Discover(ctx context.Context, timeout time.Duration) ([]*Target, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 10)
	targets := make([]*Target, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for entry := range entries {
			scheme := "http"
			for _, txt := range entry.Text {
				if txt == "scheme=https" {
					scheme = "https"
				}
			}

			addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
			for _, ip := range entry.AddrIPv4 {
				addresses = append(addresses, ip.String())
			}
			for _, ip := range entry.AddrIPv6 {
				addresses = append(addresses, ip.String())
			}

			mu.Lock()
			targets = append(targets, &Target{
				Name:      entry.Instance,
				Host:      entry.HostName,
				Port:      entry.Port,
				Addresses: addresses,
				Scheme:    scheme,
			})
			mu.Unlock()
		}
	}()

	discoverCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := resolver.Browse(discoverCtx, ServiceType, ServiceDomain, entries); err != nil {
		close(entries)
		wg.Wait()
		return nil, fmt.Errorf("failed to browse: %w", err)
	}

	<-discoverCtx.Done()
	wg.Wait()

	return targets, nil
}
