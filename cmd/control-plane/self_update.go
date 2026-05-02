package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/security"
)

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
	token := flags.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub token for private repos (env: GITHUB_TOKEN)")
	force := flags.Bool("force", false, "Force update even if versions match")
	if err := flags.Parse(args); err != nil {
		return selfUpdateOptions{}, err
	}
	return selfUpdateOptions{
		version: *version,
		repo:    *repo,
		token:   *token,
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

// downloadAndVerifySelfUpdateArchive downloads the archive + signature
// (and checksum, if available) for the chosen release and verifies both
// before returning the local archive path. Caller is responsible for
// removing the path.
func downloadAndVerifySelfUpdateArchive(ctx context.Context, panel *server.GitHubRelease, token string) (string, error) {
	binaryURL, checksumURL, signatureURL := server.ResolveAssetURLs(panel, "control-plane")
	if binaryURL == "" {
		return "", errors.New("no binary download URL found for the current platform")
	}
	if signatureURL == "" {
		return "", errors.New("release is missing a .sig asset; refusing to install unsigned binary")
	}

	var expectedChecksum string
	if checksumURL != "" {
		var err error
		expectedChecksum, err = server.DownloadChecksum(ctx, checksumURL, token)
		if err != nil {
			return "", fmt.Errorf("download checksum: %w", err)
		}
		fmt.Println("Checksum downloaded.")
	}

	archivePath, err := server.DownloadArchive(ctx, binaryURL, token)
	if err != nil {
		return "", fmt.Errorf("download archive: %w", err)
	}
	fmt.Println("Archive downloaded.")

	if err := verifySelfUpdateArchive(ctx, archivePath, signatureURL, expectedChecksum, token); err != nil {
		// G703 false positive: archivePath is the temp file we just created
		// inside DownloadArchive (via os.MkdirTemp + filepath.Join), not a
		// caller-supplied path.
		_ = os.Remove(archivePath) //nolint:gosec
		return "", err
	}
	return archivePath, nil
}

func verifySelfUpdateArchive(ctx context.Context, archivePath, signatureURL, expectedChecksum, token string) error {
	// Mandatory signature verification: fetch the detached signature, verify
	// against the embedded public key before any checksum or extraction.
	sigBytes, err := server.DownloadSignature(ctx, signatureURL, token)
	if err != nil {
		return fmt.Errorf("download signature: %w", err)
	}
	archiveBytes, err := os.ReadFile(archivePath) //nolint:gosec // archivePath from os.CreateTemp
	if err != nil {
		return fmt.Errorf("read archive for signature: %w", err)
	}
	if err := security.VerifyArtifactBytes(archiveBytes, sigBytes); err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}
	fmt.Println("Signature verified.")

	if expectedChecksum != "" {
		if err := server.VerifyChecksum(archivePath, expectedChecksum); err != nil {
			return fmt.Errorf("verify checksum: %w", err)
		}
		fmt.Println("Checksum verified.")
	}
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
