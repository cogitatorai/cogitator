package connector

import "embed"

//go:embed defaults/*
var embeddedDefaults embed.FS

// EmbeddedDefaults returns the embedded filesystem containing default connector manifests.
func EmbeddedDefaults() embed.FS {
	return embeddedDefaults
}
