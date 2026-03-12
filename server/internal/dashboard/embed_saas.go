//go:build saas

package dashboard

import "io/fs"

// FS returns nil in SaaS mode. Dashboard is served from /public on the Fly Volume.
func FS() fs.FS {
	return nil
}
