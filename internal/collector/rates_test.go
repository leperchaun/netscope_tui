package collector

import (
	"testing"
	"time"
)

func TestSocketRatesFromByteDeltas(t *testing.T) {
	s := NewSampler(nil, nil, "example.com")
	row := func(rx, tx uint64) []socketRow {
		return []socketRow{{PID: 0, LocalIP: "10.0.0.5", LocalPort: 50000, RemoteIP: "10.0.0.9", RemotePort: 443,
			State: "ESTABLISHED", Rx: rx, Tx: tx, HasIO: true, RTTms: -1}}
	}
	t0 := time.Unix(1000, 0)

	var first Snapshot
	first.TCPStates = map[string]int{}
	s.sockets(&first, t0, row(1000, 100))
	if first.Conns[0].RxBps != 0 {
		t.Fatalf("first sample should have no rate, got %v", first.Conns[0].RxBps)
	}

	var second Snapshot
	second.TCPStates = map[string]int{}
	s.sockets(&second, t0.Add(2*time.Second), row(5000, 300))
	c := second.Conns[0]
	if c.RxBps != 2000 || c.TxBps != 100 {
		t.Fatalf("want 2000/100 B/s, got %v/%v", c.RxBps, c.TxBps)
	}
	if !c.HasIO {
		t.Fatal("HasIO should be set when byte counters are present")
	}
}
