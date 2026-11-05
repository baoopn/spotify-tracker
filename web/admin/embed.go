// Package adminui embeds the built admin dashboard (SvelteKit SPA).
//
// Build it with `pnpm --dir web/admin build` before `go build`; the Docker
// image does this in a Node build stage. Without a build only a placeholder
// page is served.
package adminui

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the built SPA rooted at its output directory.
func FS() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
