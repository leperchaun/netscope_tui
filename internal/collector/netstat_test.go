package collector

import "testing"

func TestParseNetstatRow(t *testing.T) {
	line := "tcp4       0      0  192.168.2.225.49269    17.57.144.120.5223     ESTABLISHED       153620       460120  131072  131768             apsd:147    00000 00000000 0000000000000000 00000000 00000000      0      0 000000"
	c, ok := parseNetstatRow(line)
	if !ok {
		t.Fatal("row not parsed")
	}
	if c.Status != "ESTABLISHED" || c.Pid != 147 || c.Laddr.Port != 49269 || c.Raddr.IP != "17.57.144.120" || c.Raddr.Port != 5223 {
		t.Fatalf("bad parse: %+v", c)
	}
}
