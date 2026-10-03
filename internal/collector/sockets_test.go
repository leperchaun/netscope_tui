package collector

import "testing"

func TestParseSSInfo(t *testing.T) {
	text := "ESTAB 0 0 192.168.1.5:22 192.168.1.9:51234\n" +
		"\t cubic wscale:7,7 rto:204 rtt:1.5/0.75 ato:40 mss:1448 cwnd:10 bytes_sent:12345 bytes_acked:12345 bytes_received:6789 segs_out:10\n" +
		"ESTAB 0 0 [fe80::1%eth0]:443 [fe80::2]:40000\n" +
		"\t cubic rtt:20.25/3.1 bytes_sent:100 bytes_received:200\n"
	got := parseSS(text)

	first := got[ssKey("192.168.1.5:22", "192.168.1.9:51234")]
	if first.rtt != 1.5 || first.tx != 12345 || first.rx != 6789 {
		t.Fatalf("first socket: %+v", first)
	}
	second := got[ssKey("[fe80::1%eth0]:443", "[fe80::2]:40000")]
	if second.rtt != 20.25 || second.tx != 100 || second.rx != 200 {
		t.Fatalf("second socket: %+v", second)
	}
}
