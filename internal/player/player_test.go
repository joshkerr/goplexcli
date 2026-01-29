package player

import (
	"testing"
)

func TestBuildMPVArgs(t *testing.T) {
	tests := []struct {
		name       string
		urls       []string
		socketPath string
		startPos   int
		wantSocket bool
		wantStart  bool
	}{
		{
			name:       "basic playback",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "",
			startPos:   0,
			wantSocket: false,
			wantStart:  false,
		},
		{
			name:       "with IPC socket",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv.sock",
			startPos:   0,
			wantSocket: true,
			wantStart:  false,
		},
		{
			name:       "with resume position",
			urls:       []string{"http://example.com/video.mp4"},
			socketPath: "/tmp/mpv.sock",
			startPos:   125,
			wantSocket: true,
			wantStart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMPVArgs(tt.urls, tt.socketPath, tt.startPos)

			hasSocket := false
			hasStart := false
			for _, arg := range args {
				if len(arg) > 18 && arg[:18] == "--input-ipc-server" {
					hasSocket = true
				}
				if len(arg) > 8 && arg[:8] == "--start=" {
					hasStart = true
				}
			}

			if hasSocket != tt.wantSocket {
				t.Errorf("socket flag: got %v, want %v", hasSocket, tt.wantSocket)
			}
			if hasStart != tt.wantStart {
				t.Errorf("start flag: got %v, want %v", hasStart, tt.wantStart)
			}
		})
	}
}
