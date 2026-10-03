package collector

import (
	"net"
	"runtime"
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
	RxBps   float64 `json:"rx_bps"`
	TxBps   float64 `json:"tx_bps"`
	RxTotal uint64  `json:"rx_total"`
	TxTotal uint64  `json:"tx_total"`
}

type Probe struct {
	Target string        `json:"target"`
	RTT    time.Duration `json:"rtt_ns"`
	Err    string        `json:"error,omitempty"`
}

type Conn struct {
	Process string `json:"process"`
	PID     int32  `json:"pid"`
	Local   string `json:"local"`
	Remote  string `json:"remote"`
	State   string `json:"state"`
}

type Snapshot struct {
	At         time.Time      `json:"at"`
	Interfaces []Interface    `json:"interfaces"`
	TCPStates  map[string]int `json:"tcp_states"`
	Conns      []Conn         `json:"connections"`
	DNS        []DNSProbe     `json:"dns"`
	Targets    []Probe        `json:"targets"`
	RxBps      float64        `json:"rx_bps"`
	TxBps      float64        `json:"tx_bps"`
}

type Sampler struct {
	targets   []string
	resolvers []string
	dnsName   string
	prev      map[string]gnet.IOCountersStat
	prevAt    time.Time
	procNames map[int32]string
	names     *namer
}

func NewSampler(targets, resolvers []string, dnsName string) *Sampler {
	return &Sampler{targets: targets, resolvers: resolvers, dnsName: dnsName, procNames: map[int32]string{}, names: newNamer()}
}

func (s *Sampler) Sample() (Snapshot, error) {
	now := time.Now()
	snap := Snapshot{At: now, TCPStates: map[string]int{}}

	counters, err := gnet.IOCounters(true)
	if err != nil {
		return snap, err
	}
	elapsed := now.Sub(s.prevAt).Seconds()
	current := make(map[string]gnet.IOCountersStat, len(counters))
	for _, c := range counters {
		if isLoopback(c.Name) {
			continue
		}
		current[c.Name] = c
		iface := Interface{Name: c.Name, RxTotal: c.BytesRecv, TxTotal: c.BytesSent}
		if prev, ok := s.prev[c.Name]; ok && elapsed > 0 {
			iface.RxBps = rate(prev.BytesRecv, c.BytesRecv, elapsed)
			iface.TxBps = rate(prev.BytesSent, c.BytesSent, elapsed)
		}
		snap.RxBps += iface.RxBps
		snap.TxBps += iface.TxBps
		snap.Interfaces = append(snap.Interfaces, iface)
	}
	sort.Slice(snap.Interfaces, func(i, j int) bool { return snap.Interfaces[i].Name < snap.Interfaces[j].Name })
	s.prev = current
	s.prevAt = now

	if conns, err := socketTable(); err == nil {
		snap.Conns = s.establishedConns(conns)
		for _, c := range conns {
			if c.Status != "" {
				snap.TCPStates[c.Status]++
			}
		}
	}

	snap.DNS = s.probeDNSAll()
	snap.Targets = s.probeAll()
	return snap, nil
}

// socketTable prefers the kernel socket table on macOS; lsof (used by gopsutil there) misses sockets owned by system daemons.
func socketTable() ([]gnet.ConnectionStat, error) {
	if runtime.GOOS == "darwin" {
		if conns, err := netstatConnections(); err == nil && len(conns) > 0 {
			return conns, nil
		}
	}
	return gnet.Connections("tcp")
}

func (s *Sampler) establishedConns(conns []gnet.ConnectionStat) []Conn {
	var out []Conn
	seen := map[Conn]bool{}
	for _, c := range conns {
		if c.Status != "ESTABLISHED" {
			continue
		}
		conn := Conn{
			Process: s.processName(c.Pid),
			PID:     c.Pid,
			Local:   endpoint(c.Laddr.IP, c.Laddr.Port, ""),
			Remote:  endpoint(c.Raddr.IP, c.Raddr.Port, s.names.lookup(c.Raddr.IP)),
			State:   c.Status,
		}
		if seen[conn] {
			continue
		}
		seen[conn] = true
		out = append(out, conn)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Process != out[j].Process {
			return out[i].Process < out[j].Process
		}
		return out[i].Remote < out[j].Remote
	})
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
	probes := make([]Probe, len(s.targets))
	var wg sync.WaitGroup
	for i, t := range s.targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			probes[i] = probe(target)
		}(i, t)
	}
	wg.Wait()
	return probes
}

func probe(target string) Probe {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, probeTimeout)
	if err != nil {
		return Probe{Target: target, Err: err.Error()}
	}
	_ = conn.Close()
	return Probe{Target: target, RTT: time.Since(start)}
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
