// Package geo looks up country and city for IPs from a MaxMind GeoLite2 or GeoIP2 database.
package geo

import (
	"fmt"
	"net"

	"github.com/oschwald/maxminddb-golang"
)

type DB struct {
	r *maxminddb.Reader
}

// Open loads an .mmdb file. The database is not bundled; MaxMind requires a license key to download it.
func Open(path string) (*DB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open geoip database: %w", err)
	}
	return &DB{r: r}, nil
}

func (d *DB) Close() error { return d.r.Close() }

type record struct {
	Country struct {
		Names map[string]string `maxminddb:"names"`
		ISO   string            `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
}

// Lookup returns the country code and city name, or empty strings when unknown.
func (d *DB) Lookup(ip string) (country, city string) {
	parsed := net.ParseIP(ip)
	if d == nil || parsed == nil {
		return "", ""
	}
	var rec record
	if err := d.r.Lookup(parsed, &rec); err != nil {
		return "", ""
	}
	return rec.Country.ISO, rec.City.Names["en"]
}
