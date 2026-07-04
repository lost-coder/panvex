package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/server"
)

// errTokenFlagInsecure is returned when the operator passes -token without
// explicit opt-in. The flag value leaks via /proc/<pid>/cmdline to any local
// user; -token-file or the GITHUB_TOKEN env var avoid that. Mirrors
// errPasswordFlagInsecure in bootstrap_admin.go.
var errTokenFlagInsecure = errors.New(
	"--token flag exposes secrets via /proc/<pid>/cmdline; " +
		"use -token-file or GITHUB_TOKEN instead " +
		"(set PANVEX_SELF_UPDATE_ALLOW_INSECURE_TOKEN_FLAG=1 to bypass)",
)

// tokenSource captures the inputs resolveSelfUpdateToken needs to decide
// which token-supply path is safe. Mirrors passwordSource in
// bootstrap_admin.go.
type tokenSource struct {
	FlagValue     string
	FlagWasSet    bool
	FilePath      string
	EnvValue      string
	AllowInsecure bool
}

// resolveSelfUpdateToken picks the GitHub token from, in priority order:
// an explicit -token flag (gated: rejected unless AllowInsecure is set,
// since the flag value leaks via /proc/<pid>/cmdline), -token-file, then
// the GITHUB_TOKEN env var. The only token-policy error is a -token flag
// supplied without the insecure opt-in (a -token-file that cannot be read
// is surfaced too). An empty result is valid and not an error —
// self-update against a public repo needs no token.
func resolveSelfUpdateToken(src tokenSource) (string, error) {
	if src.FlagWasSet {
		if !src.AllowInsecure {
			return "", errTokenFlagInsecure
		}
		return src.FlagValue, nil
	}
	if fp := strings.TrimSpace(src.FilePath); fp != "" {
		data, err := os.ReadFile(fp)
		if err != nil {
			return "", fmt.Errorf("read token-file %q: %w", fp, err)
		}
		return strings.TrimRight(string(data), " \t\r\n"), nil
	}
	return src.EnvValue, nil
}

type selfUpdateOptions struct {
	version string
	repo    string
	token   string
	force   bool
}

func parseSelfUpdateFlags(args []string) (selfUpdateOptions, error) {
	flags := flag.NewFlagSet("self-update", flag.ContinueOnError)
	version := flags.String("version", "", "Target version to update to (e.g. 1.2.3)")
	repo := flags.String("repo", SelfUpdateRepo, "GitHub repository for release assets")
	token := flags.String("token", "", "GitHub token for private repos (leaks via /proc/<pid>/cmdline; prefer -token-file or GITHUB_TOKEN; requires PANVEX_SELF_UPDATE_ALLOW_INSECURE_TOKEN_FLAG=1)")
	tokenFile := flags.String("token-file", "", "Read GitHub token from file (preferred over -token)")
	force := flags.Bool("force", false, "Force update even if versions match")
	if err := flags.Parse(args); err != nil {
		return selfUpdateOptions{}, err
	}

	tokenFlagSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "token" {
			tokenFlagSet = true
		}
	})

	resolvedToken, err := resolveSelfUpdateToken(tokenSource{
		FlagValue:     *token,
		FlagWasSet:    tokenFlagSet,
		FilePath:      *tokenFile,
		EnvValue:      os.Getenv("GITHUB_TOKEN"),
		AllowInsecure: os.Getenv("PANVEX_SELF_UPDATE_ALLOW_INSECURE_TOKEN_FLAG") == "1",
	})
	if err != nil {
		return selfUpdateOptions{}, err
	}

	return selfUpdateOptions{
		version: *version,
		repo:    *repo,
		token:   resolvedToken,
		force:   *force,
	}, nil
}

// resolveSelfUpdateTarget fetches the latest release, picks the target version
// (CLI flag wins over latest), and returns nil/false when there is nothing to do
// (already at version / older without --force).
func resolveSelfUpdateTarget(ctx context.Context, opts selfUpdateOptions) (panel *server.GitHubRelease, targetVersion, currentVersion string, proceed bool, err error) {
	panel, _, err = server.FetchLatestVersions(ctx, opts.repo, opts.token)
	if err != nil {
		return nil, "", "", false, fmt.Errorf("fetch latest versions: %w", err)
	}
	if panel == nil {
		return nil, "", "", false, errors.New("no control-plane release found")
	}

	_, latestVersion, ok := server.ParseReleaseTag(panel.TagName)
	if !ok {
		return nil, "", "", false, fmt.Errorf("failed to parse release tag %q", panel.TagName)
	}

	targetVersion = latestVersion
	if opts.version != "" {
		targetVersion = strings.TrimPrefix(opts.version, "v")
	}

	currentVersion = strings.TrimPrefix(Version, "v")
	cmp := server.CompareVersions(targetVersion, currentVersion)
	if cmp == 0 && !opts.force {
		fmt.Printf("Already at version %s. Use --force to re-install.\n", currentVersion)
		return nil, "", "", false, nil
	}
	if cmp < 0 && !opts.force {
		fmt.Printf("Target version %s is older than current version %s. Use --force to downgrade.\n", targetVersion, currentVersion)
		return nil, "", "", false, nil
	}
	return panel, targetVersion, currentVersion, true, nil
}

// downloadAndVerifySelfUpdateArchive downloads the archive + checksum for the
// chosen release and verifies the checksum (mandatory) before returning the
// local archive path. Caller is responsible for removing the path.
func downloadAndVerifySelfUpdateArchive(ctx context.Context, panel *server.GitHubRelease, token string) (string, error) {
	binaryURL, checksumURL := server.ResolveAssetURLs(panel, "control-plane")
	if binaryURL == "" {
		return "", errors.New("no binary download URL found for the current platform")
	}
	if checksumURL == "" {
		return "", errors.New("release is missing a .sha256 asset; cannot verify integrity")
	}

	expectedChecksum, err := server.DownloadChecksum(ctx, checksumURL, token)
	if err != nil {
		return "", fmt.Errorf("download checksum: %w", err)
	}
	fmt.Println("Checksum downloaded.")

	archivePath, err := server.DownloadArchive(ctx, binaryURL, token)
	if err != nil {
		return "", fmt.Errorf("download archive: %w", err)
	}
	fmt.Println("Archive downloaded.")

	if err := verifySelfUpdateArchive(archivePath, expectedChecksum); err != nil {
		// G703 false positive: archivePath is the temp file we just created
		// inside DownloadArchive (via os.MkdirTemp + filepath.Join), not a
		// caller-supplied path.
		_ = os.Remove(archivePath) //nolint:gosec
		return "", err
	}
	return archivePath, nil
}

func verifySelfUpdateArchive(archivePath, expectedChecksum string) error {
	if err := server.VerifyChecksum(archivePath, expectedChecksum); err != nil {
		return fmt.Errorf("verify checksum: %w", err)
	}
	fmt.Println("Checksum verified.")
	return nil
}

func runSelfUpdate(args []string) error {
	opts, err := parseSelfUpdateFlags(args)
	if err != nil {
		return err
	}

	ctx := context.Background()

	panel, targetVersion, currentVersion, proceed, err := resolveSelfUpdateTarget(ctx, opts)
	if err != nil || !proceed {
		return err
	}

	fmt.Printf("Updating from %s to %s ...\n", currentVersion, targetVersion)

	archivePath, err := downloadAndVerifySelfUpdateArchive(ctx, panel, opts.token)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archivePath) }() //nolint:gosec // archivePath from os.CreateTemp, not user input

	binaryPath, err := server.ExtractBinaryFromArchive(archivePath)
	if err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}
	defer func() { _ = os.Remove(binaryPath) }() //nolint:gosec // binaryPath from os.CreateTemp
	fmt.Println("Binary extracted.")

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}

	if err := server.AtomicReplaceBinary(currentBinary, binaryPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("Updated to v%s. Restart the service to apply.\n", targetVersion)
	return nil
}
