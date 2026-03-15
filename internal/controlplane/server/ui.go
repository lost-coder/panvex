package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func newUIHandler(uiFiles fs.FS) http.HandlerFunc {
	if uiFiles == nil {
		return nil
	}

	fileServer := http.FileServer(http.FS(uiFiles))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		if isAPIPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requestPath == "" || requestPath == "." {
			serveUIIndex(w, r, uiFiles)
			return
		}

		if entry, err := fs.Stat(uiFiles, requestPath); err == nil && !entry.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		serveUIIndex(w, r, uiFiles)
	}
}

func serveUIIndex(w http.ResponseWriter, r *http.Request, uiFiles fs.FS) {
	indexFile, err := fs.ReadFile(uiFiles, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexFile)
}

func isAPIPath(requestPath string) bool {
	return requestPath == apiBasePath || strings.HasPrefix(requestPath, apiBasePath+"/")
}
