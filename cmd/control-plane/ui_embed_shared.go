package main

import "io/fs"

func resolveEmbeddedUIFiles(uiFiles fs.FS) fs.FS {
	if uiFiles == nil {
		return nil
	}

	if _, err := fs.Stat(uiFiles, "index.html"); err != nil {
		return nil
	}

	return uiFiles
}
