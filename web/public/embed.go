// Package publicui embeds the built public now-playing page (SvelteKit,
// prerendered). Build it with `pnpm --dir web/public build`; the Docker image
// does this in a Node build stage.
package publicui

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the built site rooted at its output directory.
func FS() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
