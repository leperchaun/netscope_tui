package collector

import "testing"

func TestParseNetstatRow(t *testing.T) {
	line := "tcp4       0      0  192.168.2.225.49269    17.57.144.120.5223     ESTABLISHED       153620       460120  131072  131768             apsd:147    00000 00000000 0000000000000000 00000000 00000000      0      0 000000"
	r, ok := parseNetstatRow(line)
	if !ok {
		t.Fatal("row not parsed")
	}
	if r.State != "ESTABLISHED" || r.PID != 147 || r.LocalPort != 49269 || r.RemoteIP != "17.57.144.120" || r.RemotePort != 5223 {
		t.Fatalf("bad parse: %+v", r)
	}
	if !r.HasIO || r.Rx != 153620 || r.Tx != 460120 {
		t.Fatalf("bad byte counters: %+v", r)
	}
}

func TestParseNetstatListenWildcard(t *testing.T) {
	line := "tcp4       0      0  *.5000                 *.*                    LISTEN             0          0  131072  131072  rapportd:554"
	r, ok := parseNetstatRow(line)
	if !ok || r.State != "LISTEN" || r.LocalPort != 5000 || r.LocalIP != "*" {
		t.Fatalf("listen row not parsed: %+v ok=%v", r, ok)
	}
}

func TestParseNetstatStats(t *testing.T) {
	text := "tcp:\n\t\t42 data packets (9000 bytes) retransmitted\n\t\t3 out-of-order packets (0 byte)\n\t0 segment retransmitted in RACK recovery episodes\n"
	retrans, ooo, ok := parseNetstatStats(text)
	if !ok || retrans != 42 || ooo != 3 {
		t.Fatalf("got retrans=%d ooo=%d ok=%v", retrans, ooo, ok)
	}
}

func TestSSKeyMatchesRowKey(t *testing.T) {
	r := socketRow{LocalIP: "fe80::1", LocalPort: 443, RemoteIP: "10.0.0.2", RemotePort: 51234}
	if got := ssKey("[fe80::1%eth0]:443", "10.0.0.2:51234"); got != r.key() {
		t.Fatalf("ss key %q != row key %q", got, r.key())
	}
}

func TestKeyedValue(t *testing.T) {
	snmp := "Tcp: ActiveOpens RetransSegs\nTcp: 10 42\n"
	v, ok := keyedValue(snmp, "Tcp:", "RetransSegs")
	if !ok || v != 42 {
		t.Fatalf("got %d ok=%v", v, ok)
	}
}
