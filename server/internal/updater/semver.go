package updater

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	Major, Minor, Patch int
}

// parseSemver parses a version string like "v1.2.3" or "1.2.3" into its components.
func parseSemver(s string) (semver, error) {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, "-", 2) // ignore pre-release suffix
	nums := strings.Split(parts[0], ".")
	if len(nums) != 3 {
		return semver{}, fmt.Errorf("invalid semver: %q", s)
	}
	major, err := strconv.Atoi(nums[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid major: %w", err)
	}
	minor, err := strconv.Atoi(nums[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid minor: %w", err)
	}
	patch, err := strconv.Atoi(nums[2])
	if err != nil {
		return semver{}, fmt.Errorf("invalid patch: %w", err)
	}
	return semver{Major: major, Minor: minor, Patch: patch}, nil
}

// newerThan returns true if v is strictly newer than other.
func (v semver) newerThan(other semver) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch > other.Patch
}
