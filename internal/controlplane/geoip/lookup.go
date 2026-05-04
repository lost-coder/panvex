package geoip

import (
	"fmt"
	"net"
	"strings"
)

// CityResult is the subset of GeoLite2-City fields the panel surfaces
// to operators. Empty fields mean "not present in the DB record".
type CityResult struct {
	CountryCode string
	CountryName string
	City        string
}

// ASNResult is the subset of GeoLite2-ASN fields the panel surfaces.
type ASNResult struct {
	Number       uint
	Organization string
}

// Display renders the ASN as `AS<number> <org>`, with either field
// allowed to be empty.
func (r ASNResult) Display() string {
	switch {
	case r.Number > 0 && r.Organization != "":
		return fmt.Sprintf("AS%d %s", r.Number, r.Organization)
	case r.Number > 0:
		return fmt.Sprintf("AS%d", r.Number)
	default:
		return r.Organization
	}
}

// ShouldLookup reports whether ip is worth a GeoIP query. Private,
// loopback, link-local, and unspecified addresses are never in the
// public DB and would only waste a syscall — short-circuit them.
func ShouldLookup(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip.IsPrivate() {
		return false
	}
	return true
}

// trimAndJoin is a small helper used by city decoding to flatten
// MaxMind's localised name maps into a single English string.
func trimAndJoin(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}
