package geoip

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

// Manager owns the live City and ASN readers. Lookup is RWMutex-guarded
// so reloads atomically swap the underlying readers without blocking
// in-flight requests longer than the swap itself takes.
type Manager struct {
	logger *slog.Logger

	mu   sync.RWMutex
	city *maxminddb.Reader
	asn  *maxminddb.Reader
}

// NewManager builds an empty Manager. Logger is optional — nil falls
// back to slog.Default(). The Manager owns no files until Reload is
// called.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{logger: logger}
}

// Reload opens both .mmdb files and atomically replaces the live
// readers. Either path may be empty — the corresponding reader is then
// cleared. Old readers are closed *after* the swap so concurrent
// lookups never see a half-open state.
func (m *Manager) Reload(cityPath, asnPath string) error {
	newCity, err := openIfPresent(cityPath)
	if err != nil {
		return fmt.Errorf("open city db: %w", err)
	}
	newASN, err := openIfPresent(asnPath)
	if err != nil {
		_ = closeReader(newCity)
		return fmt.Errorf("open asn db: %w", err)
	}

	m.mu.Lock()
	oldCity, oldASN := m.city, m.asn
	m.city, m.asn = newCity, newASN
	m.mu.Unlock()

	_ = closeReader(oldCity)
	_ = closeReader(oldASN)
	return nil
}

// Close releases both readers. Idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	c, a := m.city, m.asn
	m.city, m.asn = nil, nil
	m.mu.Unlock()
	errs := []error{closeReader(c), closeReader(a)}
	return errors.Join(errs...)
}

// LookupCity returns the GeoLite2-City record for ip. ok=false when no
// reader is loaded, the IP is private/loopback/etc., or the DB has no
// record for the address.
func (m *Manager) LookupCity(ip net.IP) (CityResult, bool) {
	if !ShouldLookup(ip) {
		return CityResult{}, false
	}
	m.mu.RLock()
	r := m.city
	m.mu.RUnlock()
	if r == nil {
		return CityResult{}, false
	}
	var record cityRecord
	if err := r.Lookup(ip, &record); err != nil {
		m.logger.Warn("geoip city lookup failed", "ip", ip.String(), "error", err)
		return CityResult{}, false
	}
	res := CityResult{
		CountryCode: record.Country.ISOCode,
		CountryName: record.Country.Names["en"],
		City:        record.City.Names["en"],
	}
	if res.CountryCode == "" && res.CountryName == "" && res.City == "" {
		return CityResult{}, false
	}
	return res, true
}

// LookupASN returns the GeoLite2-ASN record for ip. Same ok=false rules
// as LookupCity.
func (m *Manager) LookupASN(ip net.IP) (ASNResult, bool) {
	if !ShouldLookup(ip) {
		return ASNResult{}, false
	}
	m.mu.RLock()
	r := m.asn
	m.mu.RUnlock()
	if r == nil {
		return ASNResult{}, false
	}
	var record asnRecord
	if err := r.Lookup(ip, &record); err != nil {
		m.logger.Warn("geoip asn lookup failed", "ip", ip.String(), "error", err)
		return ASNResult{}, false
	}
	if record.AutonomousSystemNumber == 0 && strings.TrimSpace(record.AutonomousSystemOrganization) == "" {
		return ASNResult{}, false
	}
	return ASNResult{
		Number:       record.AutonomousSystemNumber,
		Organization: record.AutonomousSystemOrganization,
	}, true
}

func openIfPresent(path string) (*maxminddb.Reader, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	if err := r.Verify(); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("verify: %w", err)
	}
	return r, nil
}

func closeReader(r *maxminddb.Reader) error {
	if r == nil {
		return nil
	}
	return r.Close()
}

type cityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
}

type asnRecord struct {
	AutonomousSystemNumber       uint   `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}
