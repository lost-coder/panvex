package geoip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/oschwald/maxminddb-golang"
)

// FetchRequest describes one .mmdb download attempt. IfNoneMatch
// optionally short-circuits the download via HTTP If-None-Match.
type FetchRequest struct {
	URL         string
	Dest        string
	Kind        Kind
	IfNoneMatch string
}

// FetchResult is the outcome of a successful Fetch. NotModified=true
// means the server returned 304 and Dest was left untouched.
type FetchResult struct {
	NotModified bool
	ETag        string
	SizeBytes   int64
}

// Downloader streams URLs to disk and verifies them as MaxMind .mmdb
// files. Atomic via temp-file + rename; any error path cleans the temp.
type Downloader struct {
	client *http.Client
}

// NewDownloader wraps the given client. nil client falls back to
// http.DefaultClient.
func NewDownloader(client *http.Client) *Downloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &Downloader{client: client}
}

// Fetch streams req.URL into req.Dest, verifying the result is a valid
// .mmdb. Caller is expected to have created the parent directory
// already.
func (d *Downloader) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("build request: %w", err)
	}
	if req.IfNoneMatch != "" {
		httpReq.Header.Set("If-None-Match", req.IfNoneMatch)
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return FetchResult{}, fmt.Errorf("get %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return FetchResult{NotModified: true, ETag: req.IfNoneMatch}, nil
	}
	if resp.StatusCode/100 != 2 {
		return FetchResult{}, fmt.Errorf("get %s: status %d", req.URL, resp.StatusCode)
	}

	parent := filepath.Dir(req.Dest)
	if mkErr := os.MkdirAll(parent, 0o750); mkErr != nil {
		return FetchResult{}, fmt.Errorf("mkdir: %w", mkErr)
	}

	// CreateTemp generates a unique suffix per call (mode 0o600). This
	// matters because two concurrent fetches for the same Kind would
	// otherwise share `<dest>.tmp` — `O_TRUNC` from the second open
	// would clobber the first's in-flight bytes, leaving the verifier
	// to read a half-written file.
	tmpFile, err := os.CreateTemp(parent, filepath.Base(req.Dest)+".tmp.*")
	if err != nil {
		return FetchResult{}, fmt.Errorf("open tmp: %w", err)
	}
	tmp := tmpFile.Name()
	cleanup := func() { _ = tmpFile.Close(); _ = os.Remove(tmp) }

	n, copyErr := io.Copy(tmpFile, resp.Body)
	if copyErr != nil {
		cleanup()
		return FetchResult{}, fmt.Errorf("write tmp: %w", copyErr)
	}
	if syncErr := tmpFile.Sync(); syncErr != nil {
		cleanup()
		return FetchResult{}, fmt.Errorf("sync tmp: %w", syncErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		_ = os.Remove(tmp)
		return FetchResult{}, fmt.Errorf("close tmp: %w", closeErr)
	}

	if vErr := verifyMMDB(tmp); vErr != nil {
		_ = os.Remove(tmp)
		return FetchResult{}, fmt.Errorf("verify: %w", vErr)
	}

	if renameErr := os.Rename(tmp, req.Dest); renameErr != nil {
		_ = os.Remove(tmp)
		return FetchResult{}, fmt.Errorf("rename: %w", renameErr)
	}

	return FetchResult{
		ETag:      resp.Header.Get("ETag"),
		SizeBytes: n,
	}, nil
}

func verifyMMDB(path string) error {
	r, err := maxminddb.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Verify()
}
