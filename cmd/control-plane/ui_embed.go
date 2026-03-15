package main

import (
	"embed"
	"io/fs"
)

//go:embed ui-dist
var embeddedUIRoot embed.FS

func embeddedUIFiles() fs.FS {
	uiFiles, err := fs.Sub(embeddedUIRoot, "ui-dist")
	if err != nil {
		return nil
	}

	return resolveEmbeddedUIFiles(uiFiles)
}

func resolveEmbeddedUIFiles(uiFiles fs.FS) fs.FS {
	if uiFiles == nil {
		return nil
	}

	if _, err := fs.Stat(uiFiles, "index.html"); err != nil {
		return nil
	}

	return uiFiles
}
