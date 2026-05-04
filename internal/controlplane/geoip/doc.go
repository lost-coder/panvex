// Package geoip owns the GeoLite2 City and ASN reader lifecycle and the
// three acquisition strategies (auto, url, local). It exposes a Manager
// for lazy per-request lookup and a small Fetcher/Downloader pair the
// server's update worker drives on a schedule.
package geoip
