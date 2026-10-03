package geo

import "testing"

func TestOpenMissingDatabaseFails(t *testing.T) {
	if _, err := Open("/nonexistent/GeoLite2-City.mmdb"); err == nil {
		t.Fatal("expected an error for a missing database")
	}
}

func TestNilDBLookupIsEmpty(t *testing.T) {
	var d *DB
	if c, city := d.Lookup("8.8.8.8"); c != "" || city != "" {
		t.Fatalf("got %q %q", c, city)
	}
}
