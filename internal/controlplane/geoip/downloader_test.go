package geoip_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestDownloaderWritesAtomicallyAndVerifies(t *testing.T) {
	body := loadFixture(t, "GeoLite2-City-Test.mmdb")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "city.mmdb")

	d := geoip.NewDownloader(http.DefaultClient)
	res, err := d.Fetch(context.Background(), geoip.FetchRequest{
		URL:  srv.URL,
		Dest: dest,
		Kind: geoip.KindCity,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want %q", res.ETag, `"abc123"`)
	}
	if res.SizeBytes != int64(len(body)) {
		t.Errorf("SizeBytes = %d, want %d", res.SizeBytes, len(body))
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(got) != len(body) {
		t.Errorf("dest size = %d, want %d", len(got), len(body))
	}

	// .tmp must not survive.
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp leftover: %v", err)
	}
}

func TestDownloaderHonoursIfNoneMatch(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"abc123"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		t.Errorf("expected If-None-Match, got %q", r.Header.Get("If-None-Match"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "city.mmdb")
	// Pre-create dest so the not-modified branch must short-circuit
	// without touching it.
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	d := geoip.NewDownloader(http.DefaultClient)
	res, err := d.Fetch(context.Background(), geoip.FetchRequest{
		URL:         srv.URL,
		Dest:        dest,
		Kind:        geoip.KindCity,
		IfNoneMatch: `"abc123"`,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.NotModified {
		t.Errorf("NotModified = false, want true")
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
	if got, _ := os.ReadFile(dest); string(got) != "existing" {
		t.Errorf("dest mutated on 304 path: %q", got)
	}
}

func TestDownloaderRejectsInvalidMMDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not a real mmdb")
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "city.mmdb")

	d := geoip.NewDownloader(http.DefaultClient)
	_, err := d.Fetch(context.Background(), geoip.FetchRequest{
		URL:  srv.URL,
		Dest: dest,
		Kind: geoip.KindCity,
	})
	if err == nil {
		t.Fatal("Fetch: nil error, want verify failure")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("dest created on verify failure: %v", statErr)
	}
	if _, statErr := os.Stat(dest + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("tmp leftover: %v", statErr)
	}
}

func TestDownloaderHTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := geoip.NewDownloader(http.DefaultClient)
	_, err := d.Fetch(context.Background(), geoip.FetchRequest{
		URL:  srv.URL,
		Dest: filepath.Join(t.TempDir(), "city.mmdb"),
		Kind: geoip.KindCity,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestDownloaderConcurrentFetchesShareDestSafely guards against the
// race that surfaces when the background updater tick and a manual
// "Update now" both fire for the same Kind: with a single
// `<dest>.tmp` filename plus `O_TRUNC`, the second open clobbers the
// first's bytes mid-write. CreateTemp gives each fetch a unique
// suffix so both can complete cleanly and one rename wins.
func TestDownloaderConcurrentFetchesShareDestSafely(t *testing.T) {
	body := loadFixture(t, "GeoLite2-City-Test.mmdb")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "city.mmdb")
	d := geoip.NewDownloader(http.DefaultClient)

	const N = 6
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := d.Fetch(context.Background(), geoip.FetchRequest{
				URL:  srv.URL,
				Dest: dest,
				Kind: geoip.KindCity,
			})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Fetch[%d]: %v", i, err)
		}
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if len(got) != len(body) {
		t.Errorf("dest size = %d, want %d", len(got), len(body))
	}

	// No tmp files should survive — every successful Fetch renames its
	// own tmp away, and any losers in a same-name race wouldn't exist
	// anyway. Glob to catch the random suffix.
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp.*"))
	if len(leftovers) > 0 {
		t.Errorf("tmp leftovers: %v", leftovers)
	}
}
