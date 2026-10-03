package ui

import (
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"netscope/internal/collector"
)

const (
	historyLen = 900
	probeLen   = 300
	defaultW   = 120
	defaultH   = 44
	minW       = 80
	minH       = 24
)

type totals struct {
	rx, tx         uint64
	rxPk, txPk     uint64
	rxDrop, txDrop uint64
	retrans, ooo   uint64
}

type sampleMsg struct {
	snap collector.Snapshot
	err  error
}

type tickMsg time.Time

type Model struct {
	sampler  *collector.Sampler
	interval time.Duration

	snap   collector.Snapshot
	err    error
	width  int
	height int

	sampling bool
	paused   bool
	lite     bool
	zoom     string

	selName  string
	expanded map[string]bool

	rxHist   []float64
	txHist   []float64
	ifHist   map[string][]float64
	procHist map[string][]float64
	sockHist map[string][]float64
	probes   map[string][]collector.Probe

	have    bool
	startAt time.Time
	base    totals
	total   totals
}

func New(sampler *collector.Sampler, interval time.Duration) Model {
	return Model{
		sampler:  sampler,
		interval: interval,
		width:    defaultW,
		height:   defaultH,
		expanded: map[string]bool{},
		ifHist:   map[string][]float64{},
		procHist: map[string][]float64{},
		sockHist: map[string][]float64{},
		probes:   map[string][]collector.Probe{},
	}
}

func (m Model) Init() tea.Cmd {
	return m.sampleCmd()
}

func (m Model) sampleCmd() tea.Cmd {
	return func() tea.Msg {
		snap, err := m.sampler.Sample()
		return sampleMsg{snap: snap, err: err}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m.key(msg.String())
	case tickMsg:
		if m.sampling || m.paused {
			return m, m.tickCmd()
		}
		m.sampling = true
		return m, m.sampleCmd()
	case sampleMsg:
		m.sampling = false
		m.err = msg.err
		if msg.err == nil && !m.paused {
			m.record(msg.snap)
		}
		return m, m.tickCmd()
	}
	return m, nil
}

func (m Model) key(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "1", "2", "3", "4":
		if m.zoom == k {
			m.zoom = ""
		} else {
			m.zoom = k
		}
	case "V", "v":
		m.lite = !m.lite
	case "p":
		m.paused = !m.paused
	case "up", "k":
		m.moveSel(-1)
	case "down", "j":
		m.moveSel(1)
	case " ":
		if m.selName != "" {
			m.expanded[m.selName] = !m.expanded[m.selName]
		}
	case "z":
		m.foldAll()
	}
	return m, nil
}

func (m *Model) moveSel(delta int) {
	groups := groupsOf(m.snap.Conns)
	if len(groups) == 0 {
		return
	}
	i := m.selIndex(groups)
	i = (i + delta + len(groups)) % len(groups)
	m.selName = groups[i].name
}

func (m *Model) foldAll() {
	anyOpen := false
	for _, open := range m.expanded {
		anyOpen = anyOpen || open
	}
	for _, g := range groupsOf(m.snap.Conns) {
		m.expanded[g.name] = !anyOpen
	}
}

func (m Model) selIndex(groups []group) int {
	for i, g := range groups {
		if g.name == m.selName {
			return i
		}
	}
	return 0
}

func (m *Model) record(s collector.Snapshot) {
	m.snap = s
	m.rxHist = appendCapped(m.rxHist, s.RxBps, historyLen)
	m.txHist = appendCapped(m.txHist, s.TxBps, historyLen)

	var t totals
	for _, i := range s.Interfaces {
		if !isLoopback(i.Name) {
			t.rx += i.RxTotal
			t.tx += i.TxTotal
		}
		m.ifHist[i.Name] = appendCapped(m.ifHist[i.Name], i.RxBps+i.TxBps, historyLen)
	}
	t.rxPk, t.txPk = s.RxPackets, s.TxPackets
	t.rxDrop, t.txDrop = s.RxDrops, s.TxDrops
	t.retrans, t.ooo = s.Retrans, s.OutOfOrder
	if !m.have {
		m.base, m.have, m.startAt = t, true, s.At
	}
	m.total = t

	procRate := map[string]float64{}
	sockRate := map[string]float64{}
	for _, c := range s.Conns {
		procRate[c.Process] += c.RxBps + c.TxBps
		sockRate[c.Local+">"+c.Remote] += c.RxBps + c.TxBps
	}
	m.procHist = appendAll(m.procHist, procRate)
	m.sockHist = appendAll(m.sockHist, sockRate)
	for _, p := range s.Targets {
		m.probes[p.Target] = appendCapped(m.probes[p.Target], p, probeLen)
	}

	groups := groupsOf(s.Conns)
	found := false
	for _, g := range groups {
		found = found || g.name == m.selName
	}
	if !found && len(groups) > 0 {
		m.selName = groups[0].name
	}
}

// appendAll keeps history only for names present in this sample, so closed
// processes and sockets do not accumulate forever.
func appendAll(hist map[string][]float64, rates map[string]float64) map[string][]float64 {
	next := make(map[string][]float64, len(rates))
	for name, v := range rates {
		next[name] = appendCapped(hist[name], v, historyLen)
	}
	return next
}

type group struct {
	name  string
	pids  []int32
	conns []collector.Conn
	rx    float64
	tx    float64
	rtt   float64
	hasRT bool
	hasIO bool
}

// groupsOf collects sockets by process, busiest first.
func groupsOf(conns []collector.Conn) []group {
	idx := map[string]int{}
	var groups []group
	for _, c := range conns {
		i, ok := idx[c.Process]
		if !ok {
			i = len(groups)
			idx[c.Process] = i
			groups = append(groups, group{name: c.Process})
		}
		g := &groups[i]
		g.conns = append(g.conns, c)
		g.rx += c.RxBps
		g.tx += c.TxBps
		g.hasIO = g.hasIO || c.HasIO
		if c.RTTms >= 0 {
			g.rtt += c.RTTms
			g.hasRT = true
		}
		if !containsPID(g.pids, c.PID) {
			g.pids = append(g.pids, c.PID)
		}
	}
	for i := range groups {
		if groups[i].hasRT {
			n := 0
			for _, c := range groups[i].conns {
				if c.RTTms >= 0 {
					n++
				}
			}
			groups[i].rtt /= float64(n)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.rx+a.tx != b.rx+b.tx {
			return a.rx+a.tx > b.rx+b.tx
		}
		if len(a.conns) != len(b.conns) {
			return len(a.conns) > len(b.conns)
		}
		return a.name < b.name
	})
	return groups
}

func containsPID(pids []int32, p int32) bool {
	for _, x := range pids {
		if x == p {
			return true
		}
	}
	return false
}

func appendCapped[T any](h []T, v T, limit int) []T {
	h = append(h, v)
	if len(h) > limit {
		h = h[len(h)-limit:]
	}
	return h
}

func isLoopback(name string) bool {
	return name == "lo" || len(name) >= 3 && name[:3] == "lo0"
}
