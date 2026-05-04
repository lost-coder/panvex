package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultGitHubBaseURL is the production endpoint. Tests override.
const DefaultGitHubBaseURL = "https://api.github.com"

// DefaultRepo is the upstream auto-mode source.
const DefaultRepo = "P3TERX/GeoLite.mmdb"

// DefaultTag is the rolling release tag P3TERX overwrites weekly.
const DefaultTag = "download"

// Fetcher resolves asset download URLs from a GitHub release. Hard-coded
// to the P3TERX/GeoLite.mmdb repo + rolling `download` tag — that pair
// IS the auto-mode contract.
type Fetcher struct {
	client  *http.Client
	baseURL string
}

// NewFetcher constructs a Fetcher. baseURL is the GitHub API base
// (https://api.github.com in production); tests pass an httptest
// server URL.
func NewFetcher(client *http.Client, baseURL string) *Fetcher {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = DefaultGitHubBaseURL
	}
	return &Fetcher{client: client, baseURL: baseURL}
}

// AssetURL returns the browser_download_url for the asset matching k
// in the rolling release. Returns an error if the asset is missing
// from the release payload.
func (f *Fetcher) AssetURL(ctx context.Context, k Kind) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/tags/%s", f.baseURL, DefaultRepo, DefaultTag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("get release: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode release: %w", err)
	}

	wanted := assetName(k)
	for _, a := range release.Assets {
		if a.Name == wanted {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset %q not in release", wanted)
}

func assetName(k Kind) string {
	switch k {
	case KindCity:
		return "GeoLite2-City.mmdb"
	case KindASN:
		return "GeoLite2-ASN.mmdb"
	default:
		return ""
	}
}
