package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func newUIHandler(uiFiles fs.FS, rootPath string) http.HandlerFunc {
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
			serveUIIndex(w, r, uiFiles, rootPath)
			return
		}

		if entry, err := fs.Stat(uiFiles, requestPath); err == nil && !entry.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		serveUIIndex(w, r, uiFiles, rootPath)
	}
}

func serveUIIndex(w http.ResponseWriter, r *http.Request, uiFiles fs.FS, rootPath string) {
	indexFile, err := fs.ReadFile(uiFiles, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := string(indexFile)
	if rootPath != "" {
		body = strings.ReplaceAll(body, `src="/assets/`, `src="`+rootPath+`/assets/`)
		body = strings.ReplaceAll(body, `href="/assets/`, `href="`+rootPath+`/assets/`)
	}

	if rootPath != "" {
		rootPathJSON, err := json.Marshal(rootPath)
		if err != nil {
			http.Error(w, "failed to render ui bootstrap", http.StatusInternalServerError)
			return
		}
		rootPathScript := `<script>window.__PANVEX_ROOT_PATH=` + string(rootPathJSON) + `;</script>`
		if strings.Contains(body, "</head>") {
			body = strings.Replace(body, "</head>", rootPathScript+"</head>", 1)
		} else if strings.Contains(body, "<body>") {
			body = strings.Replace(body, "<body>", "<body>"+rootPathScript, 1)
		} else {
			body = rootPathScript + body
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func isAPIPath(requestPath string) bool {
	return requestPath == apiBasePath || strings.HasPrefix(requestPath, apiBasePath+"/")
}

func stripRootPath(rootPath string, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rootPath && !strings.HasPrefix(r.URL.Path, rootPath+"/") {
			http.NotFound(w, r)
			return
		}

		cloned := r.Clone(r.Context())
		cloned.URL.Path = strings.TrimPrefix(r.URL.Path, rootPath)
		if cloned.URL.Path == "" {
			cloned.URL.Path = "/"
		}

		next.ServeHTTP(w, cloned)
	}
}
