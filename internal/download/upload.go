package download

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	rclone "github.com/joshkerr/rclone-golib"
)

// obscurePassword runs `rclone obscure` to convert a plaintext password into
// the obscured form that rclone's backend flags (e.g. --webdav-pass) require.
// rcloneBinary defaults to "rclone" when empty. An empty password returns "".
func obscurePassword(rcloneBinary, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if rcloneBinary == "" {
		rcloneBinary = "rclone"
	}
	out, err := exec.Command(rcloneBinary, "obscure", plaintext).Output()
	if err != nil {
		return "", fmt.Errorf("failed to obscure webdav password: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// UploadToWebDAV pushes one or more rclone remote files onto a WebDAV server
// via rclone's native WebDAV backend, displaying the same Bubble Tea progress
// UI used by DownloadMultiple.
//
// rclonePaths are rclone remote source paths (the same media.RclonePath values
// used for downloads). baseURL is the WebDAV root, e.g. "http://192.168.1.10:8080".
// user/pass are the server's Basic Auth credentials (pass may be empty for
// anonymous servers). remoteDir is an optional sub-path under the server root.
// vendor is the rclone WebDAV vendor; empty defaults to "other", which suits
// generic servers such as gowebdav.
func UploadToWebDAV(ctx context.Context, rclonePaths []string, baseURL, user, pass, remoteDir, vendor, rcloneBinary string) error {
	if len(rclonePaths) == 0 {
		return fmt.Errorf("no rclone paths provided")
	}
	if baseURL == "" {
		return fmt.Errorf("webdav base URL is empty")
	}
	if rcloneBinary == "" {
		rcloneBinary = "rclone"
	}

	// The rclone-golib executor always invokes "rclone" from PATH, so verify it
	// is present (mirrors DownloadMultiple).
	if _, err := exec.LookPath(rcloneBinary); err != nil {
		return fmt.Errorf("rclone not found in PATH. Please install rclone or specify the path in config")
	}

	// The password flag requires an obscured value.
	obscured, err := obscurePassword(rcloneBinary, pass)
	if err != nil {
		return err
	}

	if vendor == "" {
		vendor = "other"
	}
	backendFlags := []string{
		"--webdav-url", baseURL,
		"--webdav-vendor", vendor,
		"--ignore-checksum",
	}
	if user != "" {
		backendFlags = append(backendFlags, "--webdav-user", user)
	}
	if obscured != "" {
		backendFlags = append(backendFlags, "--webdav-pass", obscured)
	}

	// Build transfers: copy each source to ":webdav:<remoteDir>/<filename>".
	manager := rclone.NewManager()
	type job struct {
		id   string
		src  string
		dest string
	}
	jobs := make([]job, 0, len(rclonePaths))
	for i, src := range rclonePaths {
		filename := path.Base(src)
		dest := ":webdav:" + path.Join(remoteDir, filename)
		id := generateTransferID(i, filename)
		jobs = append(jobs, job{id: id, src: src, dest: dest})
		manager.Add(id, src, dest)
	}

	// Start the Bubble Tea progress UI in a goroutine (same pattern as DownloadMultiple).
	var wg sync.WaitGroup
	var uiErr error
	uiReady := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		p := tea.NewProgram(rclone.NewModel(manager))
		close(uiReady)
		if _, err := p.Run(); err != nil {
			uiErr = err
		}
	}()
	<-uiReady

	executor := rclone.NewExecutor(manager)

	var firstErr error
	for _, j := range jobs {
		manager.Start(j.id)
		opts := rclone.RcloneOptions{
			Command:       rclone.RcloneCopyTo,
			Source:        j.src,
			Destination:   j.dest,
			StatsInterval: "500ms",
			Flags:         backendFlags,
			Context:       ctx,
		}
		if err := executor.Execute(j.id, opts); err != nil {
			manager.Fail(j.id, err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			manager.Complete(j.id)
		}
	}

	wg.Wait()

	if uiErr != nil {
		return fmt.Errorf("UI error: %w", uiErr)
	}
	if firstErr != nil {
		return fmt.Errorf("upload failed: %w", firstErr)
	}
	return nil
}
