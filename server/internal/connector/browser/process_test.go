package browser

import (
	"runtime"
	"testing"
)

func TestDetectChromePath(t *testing.T) {
	path := DetectChromePath("")
	if runtime.GOOS == "darwin" && path == "" {
		t.Skip("Chrome not installed, skipping")
	}
	if path != "" {
		t.Logf("detected Chrome at: %s", path)
	}
}

func TestDetectChromePathOverride(t *testing.T) {
	path := DetectChromePath("/usr/bin/fake-chrome")
	if path != "/usr/bin/fake-chrome" {
		t.Errorf("expected override path, got %q", path)
	}
}
