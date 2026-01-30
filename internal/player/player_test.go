package player

import (
	"testing"
)

func TestBuildMPVArgs(t *testing.T) {
	tests := []struct {
		name       string
		urls       []string
		ipcAddress string
		startPos   int
		wantIPC    bool
		wantStart  bool
	}{
		{
			name:       "basic playback",
			urls:       []string{"http://example.com/video.mp4"},
			ipcAddress: "",
			startPos:   0,
			wantIPC:    false,
			wantStart:  false,
		},
		{
			name:       "with IPC address",
			urls:       []string{"http://example.com/video.mp4"},
			ipcAddress: "127.0.0.1:19000",
			startPos:   0,
			wantIPC:    true,
			wantStart:  false,
		},
		{
			name:       "with resume position",
			urls:       []string{"http://example.com/video.mp4"},
			ipcAddress: "127.0.0.1:19000",
			startPos:   125,
			wantIPC:    true,
			wantStart:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildMPVArgs(tt.urls, tt.ipcAddress, tt.startPos)

			hasIPC := false
			hasStart := false
			for _, arg := range args {
				if len(arg) > 18 && arg[:18] == "--input-ipc-server" {
					hasIPC = true
				}
				if len(arg) > 8 && arg[:8] == "--start=" {
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
