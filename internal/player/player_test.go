package player

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildMPVArgs(t *testing.T) {
	tests := []struct {
		name       string
		urls       []string
		socketPath string
		startPos   int
		wantIPC    bool
		wantStart  bool
	}{
		{
			name:       "basic playback",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "",
			startPos:   0,
			wantIPC:    false,
			wantStart:  false,
		},
		{
			name:       "with socket path",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv-12345.sock",
			startPos:   0,
			wantIPC:    true,
			wantStart:  false,
		},
		{
			name:       "with resume position",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv-12345.sock",
			startPos:   125,
			wantIPC:    true,
			wantStart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMPVArgs(tt.urls, tt.socketPath, tt.startPos)

			hasIPC := false
			hasStart := false
			for _, arg := range args {
				if strings.HasPrefix(arg, "--input-ipc-server") {
					hasIPC = true
				}
				if strings.HasPrefix(arg, "--start=") {
					hasStart = true
				}
			}

			if hasIPC != tt.wantIPC {
				t.Errorf("IPC flag: got %v, want %v", hasIPC, tt.wantIPC)
			}
			if hasStart != tt.wantStart {
				t.Errorf("start flag: got %v, want %v", hasStart, tt.wantStart)
			}
		})
	}
}

// stubMPV writes an executable shell script that prints the given stderr lines
// and exits with the given code, standing in for the real mpv binary.
func stubMPV(t *testing.T, exitCode int, stderrLines []string) string {
	t.Helper()
	script := "#!/bin/sh\n"
	for _, l := range stderrLines {
		script += "echo '" + l + "' >&2\n"
	}
	script += "exit " + itoa(exitCode) + "\n"
	return writeStub(t, script)
}

// writeStub writes an executable shell script standing in for mpv.
func writeStub(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub not supported on windows")
	}
	path := filepath.Join(t.TempDir(), "mpv-stub")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestPlayWithMPVReportsFailure(t *testing.T) {
	stub := stubMPV(t, 2, []string{
		"Playing: https://example.com/video",
		"Failed to open https://example.com/video.",
	})
	_, err := playWithMPV(stub, []string{"https://example.com/video"}, PlaybackOptions{})
	var perr *PlaybackError
	if !errors.As(err, &perr) {
		t.Fatalf("want *PlaybackError, got %v", err)
	}
	if perr.ExitCode != 2 {
		t.Errorf("ExitCode: got %d, want 2", perr.ExitCode)
	}
	if perr.Detail != "Failed to open https://example.com/video." {
		t.Errorf("Detail: got %q", perr.Detail)
	}
	if !strings.Contains(perr.Error(), "mpv exited 2") {
		t.Errorf("Error(): got %q, want it to mention exit code", perr.Error())
	}
	if !strings.Contains(perr.Error(), "Failed to open") {
		t.Errorf("Error(): got %q, want it to include the detail", perr.Error())
	}
}

func TestPlayWithMPVCleanExitIsNotError(t *testing.T) {
	stub := stubMPV(t, 0, []string{"Exiting... (Quit)"})
	outcome, err := playWithMPV(stub, []string{"https://example.com/video"}, PlaybackOptions{})
	if err != nil {
		t.Errorf("exit 0: got %v, want nil", err)
	}
	// The outcome still carries diagnostics so callers can spot streams that
	// "played" without ever starting.
	if outcome == nil || outcome.ExitCode != 0 {
		t.Fatalf("outcome: got %+v, want ExitCode 0", outcome)
	}
	if outcome.ErrorLine != "Exiting... (Quit)" {
		t.Errorf("ErrorLine: got %q", outcome.ErrorLine)
	}
}

func TestPlayWithMPVUnknownExitCodeIsNotError(t *testing.T) {
	// Normal exits outside mpv's documented 1-3 failure range keep the old
	// lenient behavior, but the outcome reports the code.
	stub := stubMPV(t, 130, nil)
	outcome, err := playWithMPV(stub, []string{"https://example.com/video"}, PlaybackOptions{})
	if err != nil {
		t.Errorf("exit 130: got %v, want nil", err)
	}
	if outcome == nil || outcome.ExitCode != 130 {
		t.Fatalf("outcome: got %+v, want ExitCode 130", outcome)
	}
}

func TestPlayWithMPVSignalDeathIsError(t *testing.T) {
	stub := writeStub(t, "#!/bin/sh\necho 'Some stderr context' >&2\nkill -SEGV $$\n")
	outcome, err := playWithMPV(stub, []string{"https://example.com/video"}, PlaybackOptions{})
	var perr *PlaybackError
	if !errors.As(err, &perr) {
		t.Fatalf("want *PlaybackError, got %v", err)
	}
	if perr.Signal == "" {
		t.Error("Signal: got empty, want the signal name")
	}
	if !strings.Contains(perr.Error(), "mpv died") || !strings.Contains(perr.Error(), perr.Signal) {
		t.Errorf("Error(): got %q, want it to mention the signal", perr.Error())
	}
	if perr.Detail != "Some stderr context" {
		t.Errorf("Detail: got %q", perr.Detail)
	}
	if outcome == nil || outcome.Signal != perr.Signal {
		t.Errorf("outcome: got %+v, want Signal %q", outcome, perr.Signal)
	}
}

func TestErrorLineFromStderr(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: "picks last error-ish line",
			lines: []string{
				"Playing: https://example.com/video",
				"Failed to recognize file format.",
				" (+) Video --vid=1",
			},
			want: "Failed to recognize file format.",
		},
		{
			name: "falls back to last non-empty line",
			lines: []string{
				"Playing: https://example.com/video",
				"Exiting... (Quit)",
				"",
			},
			want: "Exiting... (Quit)",
		},
		{
			name:  "empty stderr",
			lines: nil,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorLineFromStderr(tt.lines); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorLineFromStderrRedactsPlexToken(t *testing.T) {
	got := errorLineFromStderr([]string{
		"Failed to open https://plex.example/video?X-Plex-Token=super-secret&quality=original.",
	})
	want := "Failed to open https://plex.example/video?X-Plex-Token=[REDACTED]&quality=original."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
