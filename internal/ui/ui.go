package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"netscope/internal/collector"
)

const (
	historyLen  = 900
	axisWidth   = 6
	defaultW    = 120
	defaultH    = 44
	minW        = 100
	minH        = 30
	sparkWidth  = 12
	budgetCells = 10
	budgetMaxMS = 200.0
	maxConnRows = 40
)

// Three vocabularies, never mixed: ui (accent/keys/muted), status (health only), series (rx/tx).
var (
	accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	bold     = lipgloss.NewStyle().Bold(true)
	accentB  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	muted    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	border   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	okStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	badStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	rx       = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	tx       = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
)

type sampleMsg struct {
	snap collector.Snapshot
	err  error
}

type tickMsg time.Time

type Model struct {
	sampler  *collector.Sampler
	interval time.Duration
	snap     collector.Snapshot
	err      error
	rxHist   []float64
	txHist   []float64
	ifHist   map[string][]float64
	rxTotal  uint64
	txTotal  uint64
	baseRx   uint64
	baseTx   uint64
	haveBase bool
	width    int
	height   int
	sampling bool
}

func New(sampler *collector.Sampler, interval time.Duration) Model {
	return Model{sampler: sampler, interval: interval, width: defaultW, height: defaultH, ifHist: map[string][]float64{}}
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
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tickMsg:
		if m.sampling {
			return m, m.tickCmd()
		}
		m.sampling = true
		return m, m.sampleCmd()
	case sampleMsg:
		m.sampling = false
		m.err = msg.err
		if msg.err == nil {
			m.record(msg.snap)
		}
		return m, m.tickCmd()
	}
	return m, nil
}

func (m *Model) record(s collector.Snapshot) {
	m.snap = s
	m.rxHist = appendCapped(m.rxHist, s.RxBps)
	m.txHist = appendCapped(m.txHist, s.TxBps)
	var rxTotal, txTotal uint64
	for _, i := range s.Interfaces {
		rxTotal += i.RxTotal
		txTotal += i.TxTotal
		m.ifHist[i.Name] = appendCapped(m.ifHist[i.Name], i.RxBps+i.TxBps)
	}
	if !m.haveBase {
		m.baseRx, m.baseTx, m.haveBase = rxTotal, txTotal, true
	}
	m.rxTotal, m.txTotal = rxTotal, txTotal
}

func (m Model) View() string {
	w, h := m.width, m.height
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	if m.snap.At.IsZero() {
		if m.err != nil {
			return badStyle.Render("sampling failed: "+m.err.Error()) + "\n"
		}
		return dim.Render("collecting first sample...") + "\n"
	}

	topH := h * 42 / 100
	midH := h * 26 / 100
	botH := h - topH - midH
	leftW := w * 44 / 100

	lines := panel("1", "net", m.netMeta(), w, topH, m.netBody(w-4, topH-2), "q quit")
	lines = append(lines, joinH(
		panel("2", "ifaces", m.ifaceMeta(), leftW, midH, m.ifaceBody(leftW-4, midH-2), ""),
		panel("3", "health", m.healthMeta(), w-leftW, midH, m.healthBody(w-leftW-4), ""),
	)...)
	lines = append(lines, panel("4", "conns", m.connMeta(), w, botH, m.connBody(w-4, botH-2), "q quit")...)
	return strings.Join(lines, "\n")
}

func (m Model) netMeta() string {
	return fmt.Sprintf("%s · %s", m.snap.At.Format("15:04:05"), m.interval)
}

// netBody lays out the throughput chart: summary, mirrored graph, time axis, tx summary.
func (m Model) netBody(inner, rows int) []string {
	peakV := peak(m.rxHist, m.txHist)
	avgV := mean(m.rxHist, m.txHist)
	chartRows := rows - 3
	if chartRows < 4 {
		chartRows = 4
	}
	out := []string{
		spread(
			rx.Render("↓")+" "+bold.Render(humanRate(m.snap.RxBps)),
			muted.Render(fmt.Sprintf("peak %s   avg %s   session ↓ %s", compact(peakV), compact(avgV), humanBytes(m.rxTotal-m.baseRx))),
			inner),
	}
	chart := graph(m.rxHist, m.txHist, inner-axisWidth, chartRows, m.interval)
	for i, line := range chart {
		out = append(out, axisLabel(i, chartRows, peakV)+line)
	}
	out = append(out, strings.Repeat(" ", axisWidth)+muted.Render(xAxis(inner-axisWidth, m.interval, len(m.rxHist))))
	out = append(out, spread(
		tx.Render("↑")+" "+bold.Render(humanRate(m.snap.TxBps)),
		muted.Render(fmt.Sprintf("peak %s   avg %s   session ↑ %s", compact(peakV), compact(avgV), humanBytes(m.txTotal-m.baseTx))),
		inner))
	return out
}

func (m Model) ifaceMeta() string {
	return fmt.Sprintf("%d up", m.activeCount())
}

func (m Model) ifaceBody(inner, rows int) []string {
	out := []string{muted.Render(fmt.Sprintf("%-12s %-12s %-12s %s", "IFACE", "DOWN", "UP", "60s"))}
	for _, i := range m.snap.Interfaces {
		if i.RxTotal == 0 && i.TxTotal == 0 {
			continue
		}
		if len(out) >= rows-2 {
			break
		}
		out = append(out, fmt.Sprintf("%-12s %s %s %s",
			trunc(i.Name, 12),
			rx.Render(fmt.Sprintf("%-12s", humanRate(i.RxBps))),
			tx.Render(fmt.Sprintf("%-12s", humanRate(i.TxBps))),
			spark(m.ifHist[i.Name], sparkWidth)))
	}
	if idle := len(m.snap.Interfaces) - m.activeCount(); idle > 0 {
		out = append(out, dim.Render(fmt.Sprintf("+%d idle hidden", idle)))
	}
	return out
}

func (m Model) healthMeta() string {
	ok, total := 0, 0
	for _, d := range m.snap.DNS {
		total++
		if d.Err == "" {
			ok++
		}
	}
	for _, t := range m.snap.Targets {
		total++
		if t.Err == "" {
			ok++
		}
	}
	return fmt.Sprintf("%d/%d ok", ok, total)
}

func (m Model) healthBody(inner int) []string {
	out := []string{muted.Render(fmt.Sprintf("%-20s %-5s %-9s %s", "TARGET", "KIND", "RTT", "BUDGET"))}
	for _, d := range m.snap.DNS {
		out = append(out, healthRow(d.Server, "dns", d.RTT, d.Err != "", d.Rcode))
	}
	for _, t := range m.snap.Targets {
		out = append(out, healthRow(t.Target, "tcp", t.RTT, t.Err != "", ""))
	}
	if len(m.snap.DNS) == 0 && len(m.snap.Targets) == 0 {
		out = append(out, dim.Render("no resolvers or targets · use -targets host:port"))
	}
	return out
}

func healthRow(target, kind string, rtt time.Duration, down bool, note string) string {
	if down {
		return fmt.Sprintf("%-20s %-5s %-9s %s", trunc(target, 20), kind, badStyle.Render("timeout"), dim.Render(strings.Repeat("·", budgetCells)))
	}
	ms := float64(rtt.Microseconds()) / 1000
	style := okStyle
	switch {
	case ms >= 150:
		style = badStyle
	case ms >= 50:
		style = warn
	}
	filled := int(math.Ceil(math.Min(ms, budgetMaxMS) / budgetMaxMS * budgetCells))
	bar := style.Render(strings.Repeat("■", filled)) + dim.Render(strings.Repeat("·", budgetCells-filled))
	return fmt.Sprintf("%-20s %-5s %s %s", trunc(target, 20), kind, style.Render(fmt.Sprintf("%-9s", fmtMS(ms))), bar)
}

func (m Model) connMeta() string {
	return fmt.Sprintf("%d established", len(m.snap.Conns))
}

// connBody groups established sockets by process, netstat-style, with remote host and port.
func (m Model) connBody(inner, rows int) []string {
	out := []string{muted.Render(fmt.Sprintf("%-16s %-7s %-40s %-8s %s", "PROCESS", "PID", "REMOTE", "PORT", "STATE"))}
	groups := groupByProcess(m.snap.Conns)
	shown := 0
	for _, g := range groups {
		if len(out) >= rows {
			break
		}
		hosts := map[string]bool{}
		for _, c := range g.conns {
			hosts[hostOf(c.Remote)] = true
		}
		out = append(out, accentB.Render("▼ ")+bold.Render(trunc(g.name, 16))+
			dim.Render(fmt.Sprintf("  pid %d  %d conns · %d hosts", g.pid, len(g.conns), len(hosts))))
		for _, c := range g.conns {
			if len(out) >= rows || shown >= maxConnRows {
				break
			}
			host, port := splitRemote(c.Remote)
			out = append(out, fmt.Sprintf("  %-14s %-7s %-40s %-8s %s",
				"", "", trunc(host, 40), port, okStyle.Render("ESTAB")))
			shown++
		}
	}
	if len(m.snap.Conns) == 0 {
		out = append(out, dim.Render("no established tcp connections (or insufficient privilege)"))
	} else if len(out) < rows && shown < len(m.snap.Conns) {
		out = append(out, dim.Render(fmt.Sprintf("+%d more", len(m.snap.Conns)-shown)))
	}
	return out
}

type procGroup struct {
	name  string
	pid   int32
	conns []collector.Conn
}

func groupByProcess(conns []collector.Conn) []procGroup {
	idx := map[string]int{}
	var groups []procGroup
	for _, c := range conns {
		i, ok := idx[c.Process]
		if !ok {
			i = len(groups)
			idx[c.Process] = i
			groups = append(groups, procGroup{name: c.Process, pid: c.PID})
		}
		groups[i].conns = append(groups[i].conns, c)
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i].conns) > len(groups[j].conns) })
	return groups
}

// panel draws a rounded box: numbered title and metadata in the top border, keys in the bottom border.
func panel(num, title, meta string, width, height int, body []string, keys string) []string {
	inner := width - 4
	if inner < 1 {
		inner = 1
	}

	head := border.Render("╭─┤ ") + accentB.Render(num) + " " + bold.Render(title) + border.Render(" ├")
	tail := ""
	if meta != "" {
		tail = border.Render("┤ ") + muted.Render(meta) + border.Render(" ├─")
	}
	fill := width - lipgloss.Width(head) - lipgloss.Width(tail) - 1
	if fill < 0 {
		tail = ""
		fill = width - lipgloss.Width(head) - 1
	}
	if fill < 0 {
		fill = 0
	}
	out := []string{head + border.Render(strings.Repeat("─", fill)) + tail + border.Render("╮")}

	rowsWanted := height - 2
	for i := 0; i < rowsWanted; i++ {
		content := ""
		if i < len(body) {
			content = lipgloss.NewStyle().MaxWidth(inner).Render(body[i])
		}
		pad := inner - lipgloss.Width(content)
		if pad < 0 {
			pad = 0
		}
		out = append(out, border.Render("│ ")+content+strings.Repeat(" ", pad)+border.Render(" │"))
	}

	foot := ""
	if keys != "" {
		foot = border.Render("╰─┤ ") + keyLine(keys) + border.Render(" ├")
	} else {
		foot = border.Render("╰─")
	}
	fillB := width - lipgloss.Width(foot) - 1
	if fillB < 0 {
		fillB = 0
	}
	out = append(out, foot+border.Render(strings.Repeat("─", fillB))+border.Render("╯"))
	return out
}

// keyLine renders "key label  key label" pairs: keys in accent, labels muted.
func keyLine(s string) string {
	parts := strings.Split(s, "  ")
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("  ")
		}
		if k, label, ok := strings.Cut(p, " "); ok {
			b.WriteString(accentB.Render(k) + " " + muted.Render(label))
		} else {
			b.WriteString(accentB.Render(p))
		}
	}
	return b.String()
}

// joinH places panels side by side; each block has equal height by construction.
func joinH(blocks ...[]string) []string {
	height := 0
	for _, b := range blocks {
		if len(b) > height {
			height = len(b)
		}
	}
	var out []string
	for row := 0; row < height; row++ {
		var sb strings.Builder
		for _, b := range blocks {
			if row < len(b) {
				sb.WriteString(b[row])
			}
		}
		out = append(out, sb.String())
	}
	return out
}

// graph renders a mirrored braille chart: rx grows up from the zero line, tx grows down.
func graph(rxS, txS []float64, cols, rows int, interval time.Duration) []string {
	upRows := rows / 2
	downRows := rows - upRows
	samples := cols * 2
	rxT := tail(rxS, samples)
	txT := tail(txS, samples)

	maxV := peak(rxS, txS)
	if maxV <= 0 {
		maxV = 1
	}

	bits := make([][]uint8, rows)
	for r := range bits {
		bits[r] = make([]uint8, cols)
	}
	// Braille dot bits indexed [subcolumn][subrow].
	dotBit := [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}
	upDots := upRows * 4
	downDots := downRows * 4

	for c := 0; c < cols; c++ {
		for s := 0; s < 2; s++ {
			i := c*2 + s
			lvl := levelOf(rxT[i], maxV, upDots)
			for k := 0; k < lvl; k++ {
				dot := upDots - 1 - k
				bits[dot/4][c] |= dotBit[s][dot%4]
			}
			lvl = levelOf(txT[i], maxV, downDots)
			for k := 0; k < lvl; k++ {
				bits[upRows+k/4][c] |= dotBit[s][k%4]
			}
		}
	}

	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		for c := 0; c < cols; c++ {
			sb.WriteRune(rune(0x2800 + int(bits[r][c])))
		}
		if r < upRows {
			out[r] = rx.Render(sb.String())
		} else {
			out[r] = tx.Render(sb.String())
		}
	}
	return out
}

// axisLabel puts y-axis labels at the top, zero line, and bottom of the chart.
func axisLabel(row, rows int, maxV float64) string {
	label := ""
	switch row {
	case 0:
		label = compact(maxV)
	case rows / 2:
		label = "0"
	case rows - 1:
		label = compact(maxV)
	}
	return muted.Render(fmt.Sprintf("%*s ", axisWidth-1, label))
}

// xAxis labels the time window (oldest on the left, "now" on the right).
func xAxis(cols int, interval time.Duration, n int) string {
	window := time.Duration(cols*2) * interval
	if n < cols*2 {
		window = time.Duration(n) * interval
	}
	left := fmt.Sprintf("-%s", fmtDuration(window))
	line := []rune(strings.Repeat(" ", cols))
	copy(line, []rune(left))
	copy(line[len(line)-3:], []rune("now"))
	return string(line)
}

func fmtDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

func (m Model) activeCount() int {
	n := 0
	for _, i := range m.snap.Interfaces {
		if i.RxTotal != 0 || i.TxTotal != 0 {
			n++
		}
	}
	return n
}

func levelOf(v, maxV float64, dots int) int {
	if v <= 0 {
		return 0
	}
	lvl := int(math.Ceil(v / maxV * float64(dots)))
	if lvl > dots {
		lvl = dots
	}
	return lvl
}

// spark renders recent values as block bars scaled to their own peak.
func spark(h []float64, width int) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	vals := tail(h, width)
	maxV := 0.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if maxV > 0 && v > 0 {
			idx = int(math.Ceil(v/maxV*float64(len(blocks)))) - 1
		}
		if idx < 0 {
			idx = 0
		}
		b.WriteRune(blocks[idx])
	}
	return dim.Render(b.String())
}

// tail returns the last n values, left-padded with zeros.
func tail(h []float64, n int) []float64 {
	out := make([]float64, n)
	if len(h) >= n {
		copy(out, h[len(h)-n:])
	} else {
		copy(out[n-len(h):], h)
	}
	return out
}

func peak(sets ...[]float64) float64 {
	max := 0.0
	for _, h := range sets {
		for _, v := range h {
			if v > max {
				max = v
			}
		}
	}
	return max
}

func mean(sets ...[]float64) float64 {
	sum, n := 0.0, 0
	for _, h := range sets {
		for _, v := range h {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// spread puts left and right at opposite ends of a row of width w.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func appendCapped(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > historyLen {
		h = h[len(h)-historyLen:]
	}
	return h
}

func splitRemote(remote string) (string, string) {
	i := strings.LastIndex(remote, ":")
	if i < 0 {
		return remote, ""
	}
	return remote[:i], remote[i+1:]
}

func hostOf(remote string) string {
	h, _ := splitRemote(remote)
	return h
}

func fmtMS(ms float64) string {
	return fmt.Sprintf("%.1fms", ms)
}

func compact(bps float64) string {
	units := []string{"", "K", "M", "G"}
	i := 0
	for bps >= 1024 && i < len(units)-1 {
		bps /= 1024
		i++
	}
	return fmt.Sprintf("%.0f%s", bps, units[i])
}

func humanRate(bps float64) string {
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	i := 0
	for bps >= 1024 && i < len(units)-1 {
		bps /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", bps, units[i])
}

func humanBytes(n uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
