//go:build embeddedui

package main

import (
	"embed"
	"io/fs"
)

//go:embed .embedded-ui
var embeddedUIRoot embed.FS

func embeddedUIFiles() fs.FS {
	uiFiles, err := fs.Sub(embeddedUIRoot, ".embedded-ui")
	if err != nil {
		return nil
	}

	return resolveEmbeddedUIFiles(uiFiles)
}
