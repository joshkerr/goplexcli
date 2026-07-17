package webdav

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// checkTimeout bounds the pre-flight connectivity check.
const checkTimeout = 10 * time.Second

// Check verifies that a WebDAV server is reachable and, when credentials are
// given, that it accepts them. It issues a PROPFIND (depth 0) against the base
// URL — the request every WebDAV server must answer — and treats an auth
// status as a credential error so callers can fail fast before a large upload.
func Check(ctx context.Context, baseURL, user, pass string) error {
	if baseURL == "" {
		return fmt.Errorf("base URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", strings.TrimRight(baseURL, "/")+"/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Depth", "0")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	client := &http.Client{Timeout: checkTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("authentication failed (HTTP %d) — check the username and password", resp.StatusCode)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	default:
		return fmt.Errorf("unexpected response: HTTP %d", resp.StatusCode)
	}
}
