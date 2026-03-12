//go:build !desktop && !saas

package dashboard

import "io/fs"

// FS returns nil when the desktop build tag is not set.
// CLI builds use disk-based dashboard serving instead.
func FS() fs.FS {
	return nil
}
