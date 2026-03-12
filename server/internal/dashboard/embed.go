//go:build desktop

package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed dist
var content embed.FS

// FS returns the embedded dashboard filesystem rooted at dist/.
func FS() fs.FS {
	sub, _ := fs.Sub(content, "dist")
	return sub
}
