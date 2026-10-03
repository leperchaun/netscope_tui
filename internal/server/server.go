package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"netscope/internal/collector"
)

// Store holds the most recent sample. Only the sampling goroutine writes to it.
type Store struct {
	mu        sync.RWMutex
	snap      collector.Snapshot
	err       error
	sampledAt time.Time
	samples   uint64
}

func (s *Store) put(snap collector.Snapshot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	if err == nil {
		s.snap = snap
		s.sampledAt = time.Now()
		s.samples++
	}
}

func (s *Store) get() (collector.Snapshot, error, time.Time, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap, s.err, s.sampledAt, s.samples
}

// Run samples on an interval until ctx is cancelled.
func Run(ctx context.Context, sampler *collector.Sampler, store *Store, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	store.put(sampler.Sample())
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			store.put(sampler.Sample())
		}
	}
}

// Handler serves /healthz, /api/snapshot, and /metrics.
func Handler(store *Store, interval time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, err, at, samples := store.get()
		fresh := !at.IsZero() && time.Since(at) < 5*interval && samples >= 2
		if !fresh {
			msg := "no fresh sample"
			if err != nil {
				msg = err.Error()
			}
			http.Error(w, msg, http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintf(w, "ok samples=%d\n", samples)
	})
	mux.HandleFunc("/api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		snap, err, _, _ := store.get()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snap)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		snap, _, _, _ := store.get()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writeMetrics(w, snap)
	})
	return mux
}

func writeMetrics(w io.Writer, s collector.Snapshot) {
	gauge := func(name, help string) { fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name) }
	counter := func(name, help string) { fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", name, help, name) }

	gauge("netscope_network_rx_bytes_per_second", "Download rate across non-loopback interfaces.")
	fmt.Fprintf(w, "netscope_network_rx_bytes_per_second %g\n", s.RxBps)
	gauge("netscope_network_tx_bytes_per_second", "Upload rate across non-loopback interfaces.")
	fmt.Fprintf(w, "netscope_network_tx_bytes_per_second %g\n", s.TxBps)

	gauge("netscope_interface_up", "1 if the interface is administratively up.")
	gauge("netscope_interface_rx_bytes_per_second", "Download rate per interface.")
	gauge("netscope_interface_tx_bytes_per_second", "Upload rate per interface.")
	counter("netscope_interface_rx_bytes_total", "Bytes received per interface.")
	counter("netscope_interface_tx_bytes_total", "Bytes sent per interface.")
	counter("netscope_interface_errors_total", "Input and output errors per interface.")
	for _, i := range s.Interfaces {
		l := label("interface", i.Name)
		fmt.Fprintf(w, "netscope_interface_up{%s} %d\n", l, b2i(i.Up))
		fmt.Fprintf(w, "netscope_interface_rx_bytes_per_second{%s} %g\n", l, i.RxBps)
		fmt.Fprintf(w, "netscope_interface_tx_bytes_per_second{%s} %g\n", l, i.TxBps)
		fmt.Fprintf(w, "netscope_interface_rx_bytes_total{%s} %d\n", l, i.RxTotal)
		fmt.Fprintf(w, "netscope_interface_tx_bytes_total{%s} %d\n", l, i.TxTotal)
		fmt.Fprintf(w, "netscope_interface_errors_total{%s} %d\n", l, i.Errors)
	}

	counter("netscope_packets_total", "Packets seen on non-loopback interfaces.")
	fmt.Fprintf(w, "netscope_packets_total{direction=\"rx\"} %d\n", s.RxPackets)
	fmt.Fprintf(w, "netscope_packets_total{direction=\"tx\"} %d\n", s.TxPackets)
	counter("netscope_packet_drops_total", "Packets dropped on non-loopback interfaces.")
	fmt.Fprintf(w, "netscope_packet_drops_total{direction=\"rx\"} %d\n", s.RxDrops)
	fmt.Fprintf(w, "netscope_packet_drops_total{direction=\"tx\"} %d\n", s.TxDrops)

	if s.HasTCPCounters {
		counter("netscope_tcp_retransmitted_segments_total", "TCP segments retransmitted since boot.")
		fmt.Fprintf(w, "netscope_tcp_retransmitted_segments_total %d\n", s.Retrans)
		counter("netscope_tcp_out_of_order_segments_total", "TCP segments received out of order since boot.")
		fmt.Fprintf(w, "netscope_tcp_out_of_order_segments_total %d\n", s.OutOfOrder)
	}

	gauge("netscope_tcp_sockets", "TCP sockets by state.")
	states := make([]string, 0, len(s.TCPStates))
	for st := range s.TCPStates {
		states = append(states, st)
	}
	sort.Strings(states)
	for _, st := range states {
		fmt.Fprintf(w, "netscope_tcp_sockets{%s} %d\n", label("state", st), s.TCPStates[st])
	}

	gauge("netscope_process_rate_bytes_per_second", "Combined TCP rate per process over established sockets.")
	rates := map[string]float64{}
	for _, c := range s.Conns {
		rates[c.Process] += c.RxBps + c.TxBps
	}
	procs := make([]string, 0, len(rates))
	for p := range rates {
		procs = append(procs, p)
	}
	sort.Strings(procs)
	for _, p := range procs {
		fmt.Fprintf(w, "netscope_process_rate_bytes_per_second{%s} %g\n", label("process", p), rates[p])
	}

	counter("netscope_capture_packets_total", "Packets seen by the packet capture.")
	fmt.Fprintf(w, "netscope_capture_packets_total %d\n", s.Capture.Packets)
	gauge("netscope_capture_packets_per_second", "Packet rate seen by the packet capture.")
	fmt.Fprintf(w, "netscope_capture_packets_per_second %g\n", s.Capture.PPS)
	gauge("netscope_capture_bytes_per_second", "Byte rate seen by the packet capture.")
	fmt.Fprintf(w, "netscope_capture_bytes_per_second %g\n", s.Capture.BPS)
	gauge("netscope_capture_protocol_packets", "Packets seen by the capture, by protocol.")
	protos := make([]string, 0, len(s.Capture.Protocols))
	for p := range s.Capture.Protocols {
		protos = append(protos, p)
	}
	sort.Strings(protos)
	for _, p := range protos {
		fmt.Fprintf(w, "netscope_capture_protocol_packets{%s} %d\n", label("protocol", p), s.Capture.Protocols[p])
	}

	gauge("netscope_probe_up", "1 if the TCP target or gateway answered this sample.")
	gauge("netscope_probe_rtt_seconds", "TCP connect round-trip time to the target.")
	for _, p := range s.Targets {
		l := label("kind", p.Kind) + "," + label("target", p.Target)
		fmt.Fprintf(w, "netscope_probe_up{%s} %d\n", l, b2i(p.Err == ""))
		if p.Err == "" {
			fmt.Fprintf(w, "netscope_probe_rtt_seconds{%s} %g\n", l, p.RTT.Seconds())
		}
	}
	gauge("netscope_dns_up", "1 if the resolver answered the test query this sample.")
	gauge("netscope_dns_rtt_seconds", "DNS query round-trip time to the resolver.")
	for _, d := range s.DNS {
		l := label("server", d.Server)
		fmt.Fprintf(w, "netscope_dns_up{%s} %d\n", l, b2i(d.Err == ""))
		if d.Err == "" {
			fmt.Fprintf(w, "netscope_dns_rtt_seconds{%s} %g\n", l, d.RTT.Seconds())
		}
	}
}

func label(k, v string) string {
	v = strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(v)
	return fmt.Sprintf(`%s="%s"`, k, v)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
