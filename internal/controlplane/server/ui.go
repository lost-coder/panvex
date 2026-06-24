package server

import (
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// headerCacheControl is the Cache-Control header name, hoisted so the
// literal is not repeated across the asset/handler responses (go:S1192).
const headerCacheControl = "Cache-Control"

// htmlOpenTagPattern matches the opening <html> tag regardless of attributes
// already present. Used to inject data-root-path for runtime configuration
// without introducing an inline <script> (which would require CSP
// 'unsafe-inline').
var htmlOpenTagPattern = regexp.MustCompile(`(?i)<html\b([^>]*)>`)

// headOpenTagPattern matches the opening <head> tag. Used to inject a
// <base> element so runtime-emitted relative URLs (Vite's __vite__mapDeps
// preload table, which writes `link.href = "assets/..."`) resolve against
// the configured root path regardless of whether the browser URL ends with
// a trailing slash.
var headOpenTagPattern = regexp.MustCompile(`(?i)<head\b[^>]*>`)

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
			// Hashed assets under /assets/ are content-addressed by Vite,
			// so they are safe to mark immutable. Anything else (favicons,
			// public/ files) gets a short revalidate-friendly window.
			if strings.HasPrefix(requestPath, "assets/") {
				w.Header().Set(headerCacheControl, "public, max-age=31536000, immutable")
			} else {
				w.Header().Set(headerCacheControl, "public, max-age=300")
			}
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

	// Inject <base href> unconditionally. Vite is configured with
	// `base: "./"` for the embed build so the emitted index.html
	// carries relative asset URLs (`./assets/…`). On a deep-link reload
	// — e.g. /clients/<uuid> — the browser resolves `./assets/` against
	// the document URL and requests `/clients/assets/…`, which the SPA
	// fallback serves as HTML, breaking every module import with a
	// MIME-type error. A <base href> pinned to the panel root anchors
	// every relative URL at a stable prefix regardless of the current
	// path. Non-rooted deployments pin to "/"; rooted deployments
	// (CSP-restricted `/pan`-style mounts) pin to "<rootPath>/".
	basePath := rootPath
	if basePath == "" {
		basePath = "/"
	} else if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	baseTag := `<base href="` + html.EscapeString(basePath) + `">`
	if loc := headOpenTagPattern.FindStringIndex(body); loc != nil {
		body = body[:loc[1]] + baseTag + body[loc[1]:]
	} else {
		body = baseTag + body
	}

	// index.html must never be cached: it carries the script-tag
	// references to the latest hashed bundle, so a stale copy means
	// the browser keeps importing yesterday's chunks. The hashed
	// /assets/ files keep the immutable-year cache.
	w.Header().Set(headerCacheControl, "no-store")
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
