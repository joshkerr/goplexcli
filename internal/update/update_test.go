package update

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
		{"1.2.3", "v1.2.3-rc1", 0}, // pre-release suffix ignored
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	got := AssetName()
	if !strings.HasPrefix(got, "goplexcli-"+runtime.GOOS+"-"+runtime.GOARCH) {
		t.Errorf("AssetName() = %q, missing os/arch prefix", got)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(got, ".exe") {
		t.Errorf("AssetName() = %q, want .exe suffix on windows", got)
	}
}

func TestFindAsset(t *testing.T) {
	rel := &Release{Assets: []Asset{
		{Name: "goplexcli-linux-amd64"},
		{Name: "goplexcli-darwin-arm64"},
	}}
	if _, ok := rel.FindAsset("goplexcli-darwin-arm64"); !ok {
		t.Error("expected to find existing asset")
	}
	if _, ok := rel.FindAsset("nope"); ok {
		t.Error("did not expect to find missing asset")
	}
}

func TestRunDevBuildIsNoop(t *testing.T) {
	var buf bytes.Buffer
	if err := Run(context.Background(), DefaultRepo, "dev", false, &buf); err != nil {
		t.Fatalf("Run(dev) returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "development build") {
		t.Errorf("expected dev-build message, got %q", buf.String())
	}
}
