package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input   string
		want    semver
		wantErr bool
	}{
		{"v1.2.3", semver{1, 2, 3}, false},
		{"1.2.3", semver{1, 2, 3}, false},
		{"v0.0.1", semver{0, 0, 1}, false},
		{"v10.20.30", semver{10, 20, 30}, false},
		{"v1.2.3-rc1", semver{1, 2, 3}, false},
		{"v0.5.0-dirty", semver{0, 5, 0}, false},
		{"v0.5.0-3-gabcdef-dirty", semver{0, 5, 0}, false},
		{"", semver{}, true},
		{"v1.2", semver{}, true},
		{"vx.y.z", semver{}, true},
	}
	for _, tt := range tests {
		got, err := parseSemver(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSemver(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNewerThan(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v0.9.0", "v1.0.0", false},
		{"v0.5.1", "v0.5.0-dirty", true},  // dirty suffix stripped
		{"v0.5.0", "v0.5.0-dirty", false}, // same base version
	}
	for _, tt := range tests {
		a, _ := parseSemver(tt.a)
		b, _ := parseSemver(tt.b)
		if got := a.newerThan(b); got != tt.want {
			t.Errorf("%s.newerThan(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestLoadCacheRevalidates(t *testing.T) {
	tests := []struct {
		name            string
		current         string
		cachedTag       string
		cachedAvailable bool
		wantAvailable   bool
	}{
		{
			name:            "stale cache: current newer than cached latest",
			current:         "v0.5.0",
			cachedTag:       "v0.3.1",
			cachedAvailable: true,
			wantAvailable:   false,
		},
		{
			name:            "valid cache: latest newer than current",
			current:         "v0.5.0",
			cachedTag:       "v0.5.1",
			cachedAvailable: true,
			wantAvailable:   true,
		},
		{
			name:            "dirty version: latest newer",
			current:         "v0.5.0-dirty",
			cachedTag:       "v0.5.1",
			cachedAvailable: false, // cache incorrectly says false
			wantAvailable:   true,  // revalidation corrects it
		},
		{
			name:            "same version",
			current:         "v0.5.1",
			cachedTag:       "v0.5.1",
			cachedAvailable: true,
			wantAvailable:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cachePath := filepath.Join(dir, "update_cache.json")

			cache := releaseCache{
				Latest:          &ReleaseInfo{Tag: tt.cachedTag},
				UpdateAvailable: tt.cachedAvailable,
			}
			data, _ := json.Marshal(cache)
			os.WriteFile(cachePath, data, 0644)

			u := &Updater{
				cfg: Config{
					Current:   tt.current,
					CachePath: cachePath,
				},
				status: Status{Current: tt.current},
			}
			u.loadCache()

			if u.status.UpdateAvailable != tt.wantAvailable {
				t.Errorf("UpdateAvailable = %v, want %v", u.status.UpdateAvailable, tt.wantAvailable)
			}
		})
	}
}
