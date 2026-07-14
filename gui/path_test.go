package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestAugmentPath checks that the original PATH is preserved, that anything
// prepended actually exists, and that running it again adds no duplicates.
func TestAugmentPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH augmentation is a no-op on windows")
	}
	orig := "/usr/bin:/bin:/usr/sbin:/sbin"
	t.Setenv("PATH", orig)

	augmentPath()
	got := os.Getenv("PATH")
	if !strings.HasSuffix(got, orig) {
		t.Fatalf("PATH = %q, want original %q kept at the end", got, orig)
	}
	for _, d := range strings.Split(strings.TrimSuffix(got, orig), ":") {
		if d == "" {
			continue
		}
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("added %q, which is not an existing directory", d)
		}
	}

	augmentPath()
	if again := os.Getenv("PATH"); again != got {
		t.Errorf("second run changed PATH:\n first: %q\nsecond: %q", got, again)
	}
}
