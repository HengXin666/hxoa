// Package web holds the local admin panel, embedded into hxoad.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// FS is the panel's static root.
func FS() fs.FS {
	sub, _ := fs.Sub(files, "static")
	return sub
}
