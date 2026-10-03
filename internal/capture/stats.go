package capture

import (
	"sort"
	"sync"
	"time"
)

const (
	maxKeys    = 4096
	recentKeep = 40
	topN       = 12
)

type Flow struct {
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	Proto   string `json:"proto"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type Name struct {
	Name  string `json:"name"`
	Count uint64 `json:"count"`
}

type Snapshot struct {
	Enabled   bool              `json:"enabled"`
	Error     string            `json:"error,omitempty"`
	Packets   uint64            `json:"packets"`
	Bytes     uint64            `json:"bytes"`
	PPS       float64           `json:"pps"`
	BPS       float64           `json:"bps"`
	Protocols map[string]uint64 `json:"protocols"`
	Flows     []Flow            `json:"flows"`
	DNS       []Name            `json:"dns"`
	SNI       []Name            `json:"sni"`
	Recent    []Packet          `json:"recent"`
}

type flowKey struct{ src, dst, proto string }

// Stats aggregates decoded packets. Writers call Add; readers call Snapshot.
type Stats struct {
	mu        sync.Mutex
	packets   uint64
	bytes     uint64
	protos    map[string]uint64
	flows     map[flowKey]*Flow
	dns       map[string]uint64
	sni       map[string]uint64
	recent    []Packet
	lastAt    time.Time
	lastPk    uint64
	lastBytes uint64
	rateAt    time.Time
	pps, bps  float64
	err       string
	enabled   bool
}

func NewStats() *Stats {
	return &Stats{
		protos: map[string]uint64{},
		flows:  map[flowKey]*Flow{},
		dns:    map[string]uint64{},
		sni:    map[string]uint64{},
	}
}

func (s *Stats) SetError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *Stats) SetEnabled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
}

func (s *Stats) Add(p Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets++
	s.bytes += uint64(p.Len)
	s.protos[p.Proto]++

	k := flowKey{p.Src, p.Dst, p.Proto}
	f := s.flows[k]
	if f == nil {
		if len(s.flows) >= maxKeys {
			s.flows = map[flowKey]*Flow{}
		}
		f = &Flow{Src: p.Src, Dst: p.Dst, Proto: p.Proto}
		s.flows[k] = f
	}
	f.Packets++
	f.Bytes += uint64(p.Len)

	if p.DNSName != "" {
		s.count(s.dns, p.DNSName)
	}
	if p.SNI != "" {
		s.count(s.sni, p.SNI)
	}

	s.recent = append(s.recent, p)
	if len(s.recent) > recentKeep {
		s.recent = s.recent[len(s.recent)-recentKeep:]
	}
}

func (s *Stats) count(m map[string]uint64, key string) {
	if _, ok := m[key]; !ok && len(m) >= maxKeys {
		return
	}
	m[key]++
}

// Snapshot returns a copy safe to read outside the lock, with rates since the previous call.
func (s *Stats) Snapshot(now time.Time) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.rateAt.IsZero() {
		if secs := now.Sub(s.rateAt).Seconds(); secs > 0 {
			s.pps = float64(s.packets-s.lastPk) / secs
			s.bps = float64(s.bytes-s.lastBytes) / secs
		}
	}
	s.rateAt, s.lastPk, s.lastBytes = now, s.packets, s.bytes

	out := Snapshot{
		Enabled:   s.enabled,
		Error:     s.err,
		Packets:   s.packets,
		Bytes:     s.bytes,
		PPS:       s.pps,
		BPS:       s.bps,
		Protocols: map[string]uint64{},
	}
	for k, v := range s.protos {
		out.Protocols[k] = v
	}
	flows := make([]Flow, 0, len(s.flows))
	for _, f := range s.flows {
		flows = append(flows, *f)
	}
	sort.Slice(flows, func(i, j int) bool { return flows[i].Bytes > flows[j].Bytes })
	if len(flows) > topN {
		flows = flows[:topN]
	}
	out.Flows = flows
	out.DNS = topNames(s.dns)
	out.SNI = topNames(s.sni)
	out.Recent = append([]Packet(nil), s.recent...)
	return out
}

func topNames(m map[string]uint64) []Name {
	out := make([]Name, 0, len(m))
	for n, c := range m {
		out = append(out, Name{Name: n, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}
