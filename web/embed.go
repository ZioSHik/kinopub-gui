// Package web embeds the built frontend (Vite output in web/dist) so the GUI
// ships as a single self-contained binary. Run `make web` (or build the web/
// project) to regenerate dist before `go build`.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the embedded frontend rooted at the build-output directory.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
