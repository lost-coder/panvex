package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// geoipUpdateInterval is the cadence the auto/URL refresh worker uses
// to re-check upstream sources. The P3TERX rolling release is updated
// weekly so 7d is the natural floor — anything tighter just burns
// GitHub rate-limit budget.
const geoipUpdateInterval = 7 * 24 * time.Hour

// geoipUpdateTimeout caps each per-Kind fetch attempt. The downloader
// streams to a temp file so a stalled response never holds memory; the
// timeout exists only to keep the worker loop responsive to shutdown
// and to surface a meaningful error to the operator instead of waiting
// indefinitely on a wedged endpoint.
const geoipUpdateTimeout = 60 * time.Second

// geoipUpdateInitialDelay defers the first refresh attempt after boot
// so a panel restart does not hammer GitHub on every redeploy and so
// the embedded UI / HTTP listener finishes coming up before any
// network I/O races. Aligns with the design's "first run 30s after
// boot" contract.
const geoipUpdateInitialDelay = 30 * time.Second

// geoipPaths caches resolved on-disk locations used by auto/URL modes.
// Local mode reads operator-supplied paths verbatim and ignores this.
type geoipPaths struct {
	// dir is the root the PathFor helper combines with the per-Kind
	// filename. Resolved once at boot from Options.SQLitePath /
	// PANVEX_GEOIP_DIR / a generic default.
	dir string
}

// restoreGeoIPSettings loads geoip settings + state from the store and
// reloads the manager if the configured paths exist on disk. Called
// from initStoreBackedSubsystems alongside the other persisted
// settings. A missing blob is not an error — it just leaves the
// in-memory zero value (mode=disabled) in place.
func (s *Server) restoreGeoIPSettings() error {
	if s.store == nil {
		s.geoipPaths = geoipPaths{dir: s.geoipDirDefault()}
		return nil
	}
	ctx := s.Context()

	settingsBlob, err := s.store.GetGeoIPSettings(ctx)
	if err != nil {
		return err
	}
	if len(settingsBlob) > 0 {
		if err := json.Unmarshal(settingsBlob, &s.geoipSettings); err != nil {
			return err
		}
	}
	stateBlob, err := s.store.GetGeoIPState(ctx)
	if err != nil {
		return err
	}
	if len(stateBlob) > 0 {
		if err := json.Unmarshal(stateBlob, &s.geoipState); err != nil {
			return err
		}
	}

	s.geoipPaths = geoipPaths{dir: s.geoipDirDefault()}

	cityPath := s.geoipResolvedPath(geoip.KindCity)
	asnPath := s.geoipResolvedPath(geoip.KindASN)
	if cityPath != "" || asnPath != "" {
		if err := s.geoip.Reload(cityPath, asnPath); err != nil {
			// A failed reload at boot must not block the panel from
			// starting — geoip enrichment is best-effort. Log and
			// continue; the next worker tick (or operator-driven
			// reload) will retry.
			s.logger.Warn("geoip restore reload failed", "error", err)
		}
	}
	return nil
}

// geoipDirDefault picks the directory used to store auto/URL-mode
// .mmdb files. Resolution order is delegated to geoip.ResolveDir:
// PANVEX_GEOIP_DIR overrides; otherwise the SQLite DB neighbour wins
// when set; otherwise the generic /var/lib/panvex/geoip.
func (s *Server) geoipDirDefault() string {
	return geoip.ResolveDir(s.sqlitePath, "/var/lib/panvex/geoip")
}

// geoipResolvedPath returns the on-disk path for a given Kind under
// the active mode. Empty string means "not configured / disabled /
// file missing" — the manager treats a "" path as a cleared reader.
func (s *Server) geoipResolvedPath(k geoip.Kind) string {
	src := s.geoipSettings.SourceFor(k)
	if !src.Enabled {
		return ""
	}
	switch s.geoipSettings.Mode {
	case geoip.ModeAuto, geoip.ModeURL:
		p := geoip.PathFor(s.geoipPaths.dir, k)
		if !fileExists(p) {
			return ""
		}
		return p
	case geoip.ModeLocal:
		if !fileExists(src.LocalPath) {
			return ""
		}
		return src.LocalPath
	default:
		return ""
	}
}

// fileExists is a defensive os.Stat wrapper used by the path resolver.
// Any stat error (ENOENT, permission denied, broken symlink) collapses
// to "missing" so a stale state row does not feed an unreadable path
// into Manager.Reload.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// validateGeoIPSettings enforces the design's invariants. Called by
// the HTTP handler before a write goes to the store. Pure function —
// it does not touch Server state, so the handler can call it before
// taking settingsMu.
func validateGeoIPSettings(s geoip.Settings) error {
	switch s.Mode {
	case geoip.ModeDisabled, geoip.ModeAuto, geoip.ModeURL, geoip.ModeLocal:
	default:
		return errors.New("invalid mode")
	}
	if s.Mode == geoip.ModeDisabled {
		return nil
	}
	if !s.City.Enabled && !s.ASN.Enabled {
		return errors.New("at least one of city/asn must be enabled")
	}
	if s.Mode == geoip.ModeURL {
		for _, src := range []geoip.Source{s.City, s.ASN} {
			if !src.Enabled {
				continue
			}
			if err := updates.CheckDownloadURL(src.URL); err != nil {
				return err
			}
		}
	}
	if s.Mode == geoip.ModeLocal {
		for _, src := range []geoip.Source{s.City, s.ASN} {
			if !src.Enabled {
				continue
			}
			if !filepath.IsAbs(src.LocalPath) {
				return errors.New("local_path must be absolute")
			}
			if !fileExists(src.LocalPath) {
				return errors.New("local_path does not exist or is not readable")
			}
		}
	}
	return nil
}

// persistGeoIPSettings marshals s.geoipSettings and writes it through
// both the OperationalStore (for /api/settings/values consistency) and
// the raw store (for restoreGeoIPSettings boot-time reload).
// Caller holds settingsMu so the marshalled snapshot matches the
// in-memory state observed by lookups during the same critical section.
// who is the audit principal (e.g. "user:42").
func (s *Server) persistGeoIPSettings(ctx context.Context, who string) error {
	data, err := json.Marshal(s.geoipSettings)
	if err != nil {
		return err
	}
	if s.settings != nil {
		if err := s.settings.Put(ctx, map[string]string{"geoip": string(data)}, who); err != nil {
			return err
		}
	}
	if s.store != nil {
		return s.store.PutGeoIPSettings(ctx, data)
	}
	return nil
}

// persistGeoIPState marshals s.geoipState and writes it. Caller holds
// settingsMu. Errors are logged rather than returned because the
// caller is the worker goroutine, which has no useful surface to
// propagate them to — the next tick will retry.
func (s *Server) persistGeoIPState(ctx context.Context) {
	if s.store == nil {
		return
	}
	data, err := json.Marshal(s.geoipState)
	if err != nil {
		s.logger.Error("marshal geoip state", "error", err)
		return
	}
	if err := s.store.PutGeoIPState(ctx, data); err != nil {
		s.logger.Error("persist geoip state", "error", err)
	}
}

// runGeoIPUpdate fetches one Kind once. Returns the fresh SourceState
// so the caller can splice it into s.geoipState atomically under
// settingsMu. Snapshots settings + previous state under RLock so the
// network call below does not hold the mutex.
func (s *Server) runGeoIPUpdate(ctx context.Context, k geoip.Kind) geoip.SourceState {
	now := s.now().Unix()
	s.settingsMu.RLock()
	settings := s.geoipSettings
	prevState := *s.geoipState.ForKind(k)
	dir := s.geoipPaths.dir
	s.settingsMu.RUnlock()

	src := settings.SourceFor(k)
	state := prevState
	state.LastCheckedAt = now
	state.Error = ""

	if !src.Enabled || settings.Mode == geoip.ModeDisabled {
		return state
	}

	switch settings.Mode {
	case geoip.ModeLocal:
		// Local mode never downloads — record the operator path and
		// stat it so the UI has a size to display. A missing file is
		// recorded as an error so the panel surface flags it.
		state.Path = src.LocalPath
		if info, err := os.Stat(src.LocalPath); err == nil {
			state.SizeBytes = info.Size()
			state.LastUpdatedAt = now
		} else {
			state.Error = err.Error()
		}
		return state

	case geoip.ModeAuto:
		fetcher := geoip.NewFetcher(http.DefaultClient, "")
		url, err := fetcher.AssetURL(ctx, k)
		if err != nil {
			state.Error = err.Error()
			return state
		}
		return s.downloadGeoIP(ctx, k, url, prevState, dir)

	case geoip.ModeURL:
		return s.downloadGeoIP(ctx, k, src.URL, prevState, dir)
	}
	return state
}

// downloadGeoIP wraps geoip.Downloader for one Kind. On 200 returns
// the new SourceState (LastUpdatedAt + SizeBytes set to fresh values).
// On 304 the cached file is still current, so it keeps prev's
// LastUpdatedAt / SizeBytes / ETag and only bumps LastCheckedAt. On
// error keeps prev's last-known-good fields and surfaces the error
// for diagnostics.
func (s *Server) downloadGeoIP(ctx context.Context, k geoip.Kind, url string, prev geoip.SourceState, dir string) geoip.SourceState {
	now := s.now().Unix()
	dest := geoip.PathFor(dir, k)
	d := geoip.NewDownloader(http.DefaultClient)
	res, err := d.Fetch(ctx, geoip.FetchRequest{URL: url, Dest: dest, Kind: k, IfNoneMatch: prev.ETag})

	// Always start from prev so we never erase a known-good
	// LastUpdatedAt / SizeBytes by transitioning through 304 or error.
	state := prev
	state.LastCheckedAt = now
	state.Path = dest
	state.Error = ""

	if err != nil {
		state.Error = err.Error()
		return state
	}
	if res.NotModified {
		// File on disk is still current; nothing to update.
		return state
	}
	state.LastUpdatedAt = now
	state.ETag = res.ETag
	state.SizeBytes = res.SizeBytes
	return state
}

// startGeoIPUpdaterWorker spawns a single worker that drives the
// auto/URL refresh ticker. No-op for disabled and local modes.
// Caller must NOT hold settingsMu — this function takes the write
// lock to manage geoipWorkerCancel.
func (s *Server) startGeoIPUpdaterWorker(parent context.Context) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	if s.geoipWorkerCancel != nil {
		s.geoipWorkerCancel()
		s.geoipWorkerCancel = nil
	}
	mode := s.geoipSettings.Mode
	if mode != geoip.ModeAuto && mode != geoip.ModeURL {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	s.geoipWorkerCancel = cancel
	s.rollupWg.Add(1)
	go s.geoipUpdateLoop(ctx)
}

// geoipUpdateLoop runs the periodic refresh. The initial delay gives
// the rest of the panel time to come up before we hit the network;
// subsequent ticks fire every geoipUpdateInterval. Done via select on
// ctx.Done so cancellation is observed promptly.
func (s *Server) geoipUpdateLoop(ctx context.Context) {
	defer s.rollupWg.Done()

	select {
	case <-ctx.Done():
		return
	case <-time.After(geoipUpdateInitialDelay):
	}
	s.runAndPersistGeoIP(ctx)

	ticker := time.NewTicker(geoipUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAndPersistGeoIP(ctx)
		}
	}
}

// runAndPersistGeoIP runs an update for both Kinds, commits the new
// state under settingsMu, asks the manager to swap readers to match,
// and persists the state blob. Each per-Kind fetch gets its own
// timeout so a wedged endpoint cannot block the other Kind.
func (s *Server) runAndPersistGeoIP(ctx context.Context) {
	for _, k := range []geoip.Kind{geoip.KindCity, geoip.KindASN} {
		fetchCtx, cancel := context.WithTimeout(ctx, geoipUpdateTimeout)
		newState := s.runGeoIPUpdate(fetchCtx, k)
		cancel()

		s.settingsMu.Lock()
		if slot := s.geoipState.ForKind(k); slot != nil {
			*slot = newState
		}
		s.settingsMu.Unlock()
	}
	s.settingsMu.Lock()
	s.reloadGeoIPManager()
	s.persistGeoIPState(ctx)
	s.settingsMu.Unlock()
}

// reloadGeoIPManager re-resolves both database paths from the current
// settings + state and asks the manager to swap readers. Caller holds
// settingsMu (which guards the fields geoipResolvedPath reads). A
// reload error is logged, not returned, because callers are either
// boot-time or background workers — neither has a meaningful recovery
// path beyond the next tick.
func (s *Server) reloadGeoIPManager() {
	cityPath := s.geoipResolvedPath(geoip.KindCity)
	asnPath := s.geoipResolvedPath(geoip.KindASN)
	if err := s.geoip.Reload(cityPath, asnPath); err != nil {
		s.logger.Warn("geoip reload failed", "error", err)
	}
}
