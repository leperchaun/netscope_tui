package collector

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

const probeTimeout = 2 * time.Second

type Interface struct {
	Name    string  `json:"name"`
	Up      bool    `json:"up"`
	RxBps   float64 `json:"rx_bps"`
	TxBps   float64 `json:"tx_bps"`
	RxTotal uint64  `json:"rx_total"`
	TxTotal uint64  `json:"tx_total"`
	Errors  uint64  `json:"errors"`
}

type Probe struct {
	Kind   string        `json:"kind"`
	Target string        `json:"target"`
	RTT    time.Duration `json:"rtt_ns"`
	Err    string        `json:"error,omitempty"`
}

type Conn struct {
	Process string  `json:"process"`
	PID     int32   `json:"pid"`
	Local   string  `json:"local"`
	Remote  string  `json:"remote"`
	State   string  `json:"state"`
	RxBps   float64 `json:"rx_bps"`
	TxBps   float64 `json:"tx_bps"`
	RTTms   float64 `json:"rtt_ms"`
	HasIO   bool    `json:"io_known"`
}

type Snapshot struct {
	At             time.Time      `json:"at"`
	Interfaces     []Interface    `json:"interfaces"`
	TCPStates      map[string]int `json:"tcp_states"`
	Conns          []Conn         `json:"connections"`
	DNS            []DNSProbe     `json:"dns"`
	Targets        []Probe        `json:"targets"`
	RxBps          float64        `json:"rx_bps"`
	TxBps          float64        `json:"tx_bps"`
	RxPackets      uint64         `json:"rx_packets"`
	TxPackets      uint64         `json:"tx_packets"`
	RxDrops        uint64         `json:"rx_drops"`
	TxDrops        uint64         `json:"tx_drops"`
	Retrans        uint64         `json:"retrans"`
	OutOfOrder     uint64         `json:"out_of_order"`
	HasTCPCounters bool           `json:"tcp_counters"`
}

type ioCount struct{ rx, tx uint64 }

type Sampler struct {
	targets    []string
	gateway    string
	resolvers  []string
	dnsName    string
	prev       map[string]gnet.IOCountersStat
	prevAt     time.Time
	prevSock   map[string]ioCount
	prevSockAt time.Time
	procNames  map[int32]string
	names      *namer
}

func NewSampler(targets, resolvers []string, dnsName string) *Sampler {
	return &Sampler{
		targets:   targets,
		resolvers: resolvers,
		dnsName:   dnsName,
		prevSock:  map[string]ioCount{},
		procNames: map[int32]string{},
		names:     newNamer(),
		gateway:   defaultGateway(),
	}
}

func (s *Sampler) Sample() (Snapshot, error) {
	now := time.Now()
	snap := Snapshot{At: now, TCPStates: map[string]int{}}

	counters, err := gnet.IOCounters(true)
	if err != nil {
		return snap, err
	}
	up := upInterfaces()
	elapsed := now.Sub(s.prevAt).Seconds()
	current := make(map[string]gnet.IOCountersStat, len(counters))
	for _, c := range counters {
		current[c.Name] = c
		iface := Interface{
			Name:    c.Name,
			Up:      up[c.Name],
			RxTotal: c.BytesRecv,
			TxTotal: c.BytesSent,
			Errors:  c.Errin + c.Errout,
		}
		if prev, ok := s.prev[c.Name]; ok && elapsed > 0 {
			iface.RxBps = rate(prev.BytesRecv, c.BytesRecv, elapsed)
			iface.TxBps = rate(prev.BytesSent, c.BytesSent, elapsed)
		}
		if !isLoopback(c.Name) {
			snap.RxBps += iface.RxBps
			snap.TxBps += iface.TxBps
			snap.RxPackets += c.PacketsRecv
			snap.TxPackets += c.PacketsSent
			snap.RxDrops += c.Dropin
			snap.TxDrops += c.Dropout
		}
		snap.Interfaces = append(snap.Interfaces, iface)
	}
	sort.Slice(snap.Interfaces, func(i, j int) bool { return snap.Interfaces[i].Name < snap.Interfaces[j].Name })
	s.prev = current
	s.prevAt = now

	snap.Retrans, snap.OutOfOrder, snap.HasTCPCounters = tcpCounters()
	s.sockets(&snap, now, socketRows())

	snap.DNS = s.probeDNSAll()
	snap.Targets = s.probeAll()
	return snap, nil
}

// sockets fills TCP state counts and per-connection rates. Rates come from deltas of
// cumulative byte counters between samples, so the first sample reports zero.
func (s *Sampler) sockets(snap *Snapshot, now time.Time, raw []socketRow) {
	rows := dedupeRows(raw)
	elapsed := now.Sub(s.prevSockAt)
	next := make(map[string]ioCount, len(rows))
	seconds := elapsed.Seconds()

	for _, r := range rows {
		if r.State != "" {
			snap.TCPStates[r.State]++
		}
		if r.State != "ESTABLISHED" {
			continue
		}
		c := Conn{
			Process: s.processName(r.PID),
			PID:     r.PID,
			Local:   endpoint(r.LocalIP, r.LocalPort, ""),
			Remote:  endpoint(r.RemoteIP, r.RemotePort, s.names.lookup(r.RemoteIP)),
			State:   r.State,
			RTTms:   r.RTTms,
			HasIO:   r.HasIO,
		}
		if r.HasIO {
			k := r.key()
			next[k] = ioCount{rx: r.Rx, tx: r.Tx}
			if prev, ok := s.prevSock[k]; ok && seconds > 0 {
				c.RxBps = rate(prev.rx, r.Rx, seconds)
				c.TxBps = rate(prev.tx, r.Tx, seconds)
			}
		}
		snap.Conns = append(snap.Conns, c)
	}
	s.prevSock = next
	s.prevSockAt = now

	sort.Slice(snap.Conns, func(i, j int) bool {
		if snap.Conns[i].Process != snap.Conns[j].Process {
			return snap.Conns[i].Process < snap.Conns[j].Process
		}
		return snap.Conns[i].Remote < snap.Conns[j].Remote
	})
}

func dedupeRows(rows []socketRow) []socketRow {
	seen := map[string]bool{}
	out := rows[:0]
	for _, r := range rows {
		k := r.key() + "|" + r.State
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	return out
}

func endpoint(ip string, port uint32, name string) string {
	host := ip
	if name != "" && name != ip {
		host = name
	}
	return net.JoinHostPort(host, serviceName(port))
}

func (s *Sampler) processName(pid int32) string {
	if pid <= 0 {
		return "(unknown)"
	}
	if name, ok := s.procNames[pid]; ok {
		return name
	}
	name := "(unknown)"
	if p, err := process.NewProcess(pid); err == nil {
		if n, err := p.Name(); err == nil && n != "" {
			name = n
		}
	}
	s.procNames[pid] = name
	return name
}

func (s *Sampler) probeDNSAll() []DNSProbe {
	out := make([]DNSProbe, len(s.resolvers))
	var wg sync.WaitGroup
	for i, r := range s.resolvers {
		wg.Add(1)
		go func(i int, server string) {
			defer wg.Done()
			out[i] = ProbeDNS(server, s.dnsName)
		}(i, r)
	}
	wg.Wait()
	return out
}

func (s *Sampler) probeAll() []Probe {
	var targets []struct{ kind, addr string }
	if s.gateway != "" {
		targets = append(targets, struct{ kind, addr string }{"gateway", net.JoinHostPort(s.gateway, "53")})
	}
	for _, t := range s.targets {
		targets = append(targets, struct{ kind, addr string }{"tcp", t})
	}
	probes := make([]Probe, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, kind, addr string) {
			defer wg.Done()
			probes[i] = probe(kind, addr)
		}(i, t.kind, t.addr)
	}
	wg.Wait()
	return probes
}

// probe times a TCP connect. A refused connection still means the host answered, so its RTT counts.
func probe(kind, target string) Probe {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, probeTimeout)
	if err != nil && !strings.Contains(err.Error(), "refused") {
		return Probe{Kind: kind, Target: target, Err: err.Error()}
	}
	if conn != nil {
		_ = conn.Close()
	}
	return Probe{Kind: kind, Target: target, RTT: time.Since(start)}
}

func upInterfaces() map[string]bool {
	out := map[string]bool{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifs {
		out[i.Name] = i.Flags&net.FlagUp != 0
	}
	return out
}

func rate(prev, cur uint64, seconds float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / seconds
}

func isLoopback(name string) bool {
	return name == "lo" || strings.HasPrefix(name, "lo0")
}
