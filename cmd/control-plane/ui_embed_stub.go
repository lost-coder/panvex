//go:build !embeddedui

package main

import "io/fs"

func embeddedUIFiles() fs.FS {
	return nil
}
