package server

import (
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// htmlOpenTagPattern matches the opening <html> tag regardless of attributes
// already present. Used to inject data-root-path for runtime configuration
// without introducing an inline <script> (which would require CSP
// 'unsafe-inline').
var htmlOpenTagPattern = regexp.MustCompile(`(?i)<html\b([^>]*)>`)

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
		// Inject runtime configuration via a data-* attribute on <html>
		// rather than an inline <script>. Our CSP forbids 'unsafe-inline',
		// and a data attribute is read at runtime via
		// document.documentElement.dataset.rootPath (see web/src/lib/runtime-path.ts).
		escaped := html.EscapeString(rootPath)
		attr := ` data-root-path="` + escaped + `"`
		if loc := htmlOpenTagPattern.FindStringSubmatchIndex(body); loc != nil {
			existingAttrs := body[loc[2]:loc[3]]
			replacement := "<html" + existingAttrs + attr + ">"
			body = body[:loc[0]] + replacement + body[loc[1]:]
		} else {
			// No <html> tag found — fall back to prepending a minimal one so
			// the dataset lookup still works.
			body = "<html" + attr + ">" + body + "</html>"
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
