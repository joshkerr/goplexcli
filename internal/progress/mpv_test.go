package progress

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseMPVResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantData interface{}
		wantErr  bool
	}{
		{
			name:     "time-pos response",
			response: `{"data":125.432,"error":"success"}`,
			wantData: 125.432,
			wantErr:  false,
		},
		{
			name:     "pause response false",
			response: `{"data":false,"error":"success"}`,
			wantData: false,
			wantErr:  false,
		},
		{
			name:     "pause response true",
			response: `{"data":true,"error":"success"}`,
			wantData: true,
			wantErr:  false,
		},
		{
			name:     "error response",
			response: `{"data":null,"error":"property not found"}`,
			wantData: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp mpvResponse
			if err := json.Unmarshal([]byte(tt.response), &resp); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if tt.wantErr {
				if resp.Error == "success" {
					t.Error("expected error, got success")
				}
			} else {
				if resp.Error != "success" {
					t.Errorf("expected success, got %s", resp.Error)
				}
			}
		})
	}
}

func TestBuildMPVCommand(t *testing.T) {
	cmd := buildMPVCommand("get_property", "time-pos")
	expected := `{"command":["get_property","time-pos"]}`

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestGenerateIPCAddress(t *testing.T) {
	addr := GenerateIPCAddress()

	// Should be non-empty
	if len(addr) == 0 {
		t.Error("expected non-empty IPC address")
	}

	// Should be localhost address with port
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("expected address to start with '127.0.0.1:', got %s", addr)
	}
}

func TestNewMPVClient(t *testing.T) {
	address := "127.0.0.1:19000"
	client := NewMPVClient(address)

	if client.address != address {
		t.Errorf("expected address %s, got %s", address, client.address)
	}

	if client.conn != nil {
		t.Error("expected nil connection for new client")
	}
}
