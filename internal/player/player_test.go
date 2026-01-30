package player

import (
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
