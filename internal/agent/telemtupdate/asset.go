package telemtupdate

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
)

// assetName resolves a Telemt release asset name from runtime architecture,
// libc type (gnu vs musl), and optional CPU feature level (v3 for x86-64-v3, i.e. AVX2/BMI2/FMA and friends).
// Returns a name like "telemt-x86_64-v3-linux-musl" or "telemt-aarch64-linux-gnu".
// Error if goarch is unsupported (only amd64, arm64) or if flavor=="v3" is
// requested on arm64 (not available upstream).
func assetName(goarch string, musl bool, flavor string) (string, error) {
	// Map Go runtime.GOARCH to the upstream release target triple.
	var arch string
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	// arm64 does not have v3 flavor in the release.
	if goarch == "arm64" && flavor == "v3" {
		return "", errors.New("flavor v3 not available for arm64")
	}

	// Only empty flavor (baseline) or "v3" (for x86_64) are valid.
	if flavor != "" && flavor != "v3" {
		return "", fmt.Errorf("unsupported flavor: %s", flavor)
	}

	// Construct the asset name: telemt-{arch}[-{flavor}]-linux-{libc}.
	var name string
	if flavor != "" {
		name = fmt.Sprintf("telemt-%s-%s-linux-", arch, flavor)
	} else {
		name = fmt.Sprintf("telemt-%s-linux-", arch)
	}

	if musl {
		name += "musl"
	} else {
		name += "gnu"
	}

	return name, nil
}

// detectMusl checks if the system uses musl libc by searching for musl-specific
// files in /lib. The glob function allows for testing; production use passes filepath.Glob.
// Returns true if /lib/ld-musl-* or /lib/libc.musl-* exists.
func detectMusl(glob func(pattern string) ([]string, error)) bool {
	patterns := []string{"/lib/ld-musl-*", "/lib/libc.musl-*"}
	for _, pattern := range patterns {
		matches, _ := glob(pattern)
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

// ResolveAsset returns the Telemt release asset name for the current system,
// using runtime.GOARCH and filesystem detection of musl. Typical usage:
//
//	name, err := ResolveAsset("v3")
//	// name might be "telemt-x86_64-v3-linux-gnu" or "telemt-aarch64-linux-gnu"
func ResolveAsset(flavor string) (string, error) {
	musl := detectMusl(filepath.Glob)
	return assetName(runtime.GOARCH, musl, flavor)
}
