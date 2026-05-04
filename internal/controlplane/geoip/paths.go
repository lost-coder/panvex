package geoip

import (
	"os"
	"path/filepath"
	"strings"
)

const envDir = "PANVEX_GEOIP_DIR"

// ResolveDir picks the directory used to store auto/URL-mode .mmdb
// files. Resolution order:
//
//  1. PANVEX_GEOIP_DIR env var.
//  2. <dir(sqlitePath)>/geoip when the SQLite driver is in use.
//  3. genericDefault (typically /var/lib/panvex/geoip).
//
// The directory is NOT created here — runUpdate creates it lazily on
// first write so the read-only path stays side-effect free.
func ResolveDir(sqlitePath, genericDefault string) string {
	if v := strings.TrimSpace(os.Getenv(envDir)); v != "" {
		return v
	}
	if sqlitePath != "" {
		return filepath.Join(filepath.Dir(sqlitePath), "geoip")
	}
	return genericDefault
}

// PathFor returns the conventional on-disk path for a given Kind under
// dir. Used by auto and URL modes; local mode reads from operator paths
// verbatim.
func PathFor(dir string, k Kind) string {
	switch k {
	case KindCity:
		return filepath.Join(dir, "city.mmdb")
	case KindASN:
		return filepath.Join(dir, "asn.mmdb")
	default:
		return ""
	}
}
