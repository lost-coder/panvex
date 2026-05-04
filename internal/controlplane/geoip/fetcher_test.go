package geoip_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

const fakeReleasePayload = `{
  "tag_name": "download",
  "assets": [
    {"name": "GeoLite2-City.mmdb", "browser_download_url": "https://example.test/city.mmdb"},
    {"name": "GeoLite2-ASN.mmdb",  "browser_download_url": "https://example.test/asn.mmdb"},
    {"name": "GeoLite2-Country.mmdb", "browser_download_url": "https://example.test/country.mmdb"}
  ]
}`

func TestFetcherResolvesAssetURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/P3TERX/GeoLite.mmdb/releases/tags/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeReleasePayload))
	}))
	defer srv.Close()

	f := geoip.NewFetcher(http.DefaultClient, srv.URL)
	city, err := f.AssetURL(context.Background(), geoip.KindCity)
	if err != nil {
		t.Fatalf("AssetURL city: %v", err)
	}
	if city != "https://example.test/city.mmdb" {
		t.Errorf("city url = %q", city)
	}
	asn, err := f.AssetURL(context.Background(), geoip.KindASN)
	if err != nil {
		t.Fatalf("AssetURL asn: %v", err)
	}
	if asn != "https://example.test/asn.mmdb" {
		t.Errorf("asn url = %q", asn)
	}
}

func TestFetcherErrorsOnMissingAsset(t *testing.T) {
	payload := `{"tag_name":"download","assets":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	f := geoip.NewFetcher(http.DefaultClient, srv.URL)
	if _, err := f.AssetURL(context.Background(), geoip.KindCity); err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestFetcherErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	f := geoip.NewFetcher(http.DefaultClient, srv.URL)
	_, err := f.AssetURL(context.Background(), geoip.KindCity)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetcherJSONShapeMatchesGitHub(t *testing.T) {
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(fakeReleasePayload), &release); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if release.TagName != "download" || len(release.Assets) != 3 {
		t.Errorf("payload drifted: %+v", release)
	}
}
