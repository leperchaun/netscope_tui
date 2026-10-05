package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"netscope/internal/collector"
)

const (
	gutterW     = 7
	budgetCells = 25
	budgetMaxMS = 375.0
	maxRows     = 200
)

// Three vocabularies, never mixed: ui (accent/keys/muted), status (health only), series (rx/tx).
var (
	accentB  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	bold     = lipgloss.NewStyle().Bold(true)
	muted    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	border   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	okStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	badStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	rxStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	txStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
)

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

	lines := []string{m.tabBar(w)}
	if m.tab != 0 {
		return strings.Join(append(lines, m.body(w, h-1)...), "\n")
	}
	if m.lite {
		netH := clamp((h-1)*34/100, 9, h-7)
		lines = append(lines, m.netPanel(w, netH)...)
		lines = append(lines, m.connPanel(w, h-1-netH)...)
		return strings.Join(lines, "\n")
	}

	dashH := h - 1
	topH := dashH * 40 / 100
	midH := dashH * 28 / 100
	botH := dashH - topH - midH
	leftW := w * 44 / 100

	lines = append(lines, m.netPanel(w, topH)...)
	gap := make([]string, midH)
	for i := range gap {
		gap[i] = " "
	}
	lines = append(lines, joinH(
		m.ifacePanel(leftW, midH),
		gap,
		m.healthPanel(w-leftW-1, midH),
	)...)
	lines = append(lines, m.connPanel(w, botH)...)
	return strings.Join(lines, "\n")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m Model) netPanel(w, h int) []string {
	meta := fmt.Sprintf("%s · %s", m.primaryIface(), m.snap.At.Format("15:04:05"))
	keys := "V view  p pause  f freeze  r record  q quit"
	if m.paused {
		meta = warn.Render("paused") + muted.Render(" · "+meta)
		keys = "V view  p resume  f freeze  r record  q quit"
	}
	return panel("1", "net", meta, w, h, m.netBody(w-4, h-2), keys)
}

func (m Model) ifacePanel(w, h int) []string {
	return panel("2", "ifaces", fmt.Sprintf("%d active", m.activeCount()), w, h, m.ifaceBody(w-4, h-2), "")
}

func (m Model) healthPanel(w, h int) []string {
	return panel("3", "health", m.healthMeta(), w, h, m.healthBody(w-4, h-2), "")
}

func (m Model) connPanel(w, h int) []string {
	groups := groupsOf(m.visibleConns())
	meta := fmt.Sprintf("%d in %d procs · sort ↓ rate", len(m.visibleConns()), len(groups))
	return panel("4", "conns", meta, w, h, m.connBody(w-4, h-2), "↑↓ select  space fold  z fold all  / filter  q quit")
}

func (m Model) primaryIface() string {
	best, name := -1.0, "-"
	for _, i := range m.snap.Interfaces {
		if isLoopback(i.Name) || !i.Up {
			continue
		}
		if t := i.RxBps + i.TxBps; t > best {
			best, name = t, i.Name
		}
	}
	return name
}

// netBody: summary, upper half, zero axis with window label, lower half, summary.
func (m Model) netBody(inner, rows int) []string {
	chart := rows - 3
	if chart < 2 {
		chart = 2
	}
	upRows := chart / 2
	downRows := chart - upRows
	cols := inner - gutterW
	maxV := peak(m.rxHist, m.txHist)
	meanV := mean(m.rxHist, m.txHist)
	sess := m.session()

	out := []string{spread(
		rxStyle.Render("↓")+" "+bold.Render(humanRate(m.snap.RxBps)),
		muted.Render(fmt.Sprintf("peak %s · avg %s · session ↓ %s · pkts %s · drop %s",
			humanRate(maxV), humanRate(meanV), humanBytes(sess.rx), humanCount(sess.rxPk), humanCount(sess.rxDrop))),
		inner)}

	upper, lower := graph(m.rxHist, m.txHist, cols, upRows, downRows)
	for i, line := range upper {
		out = append(out, gutter(i, upRows, maxV, false)+line)
	}
	out = append(out, dim.Render(fmt.Sprintf("%*s┤", gutterW-1, fmtDuration(time.Duration(len(m.rxHist))*m.interval)))+axisLine(cols))
	for i, line := range lower {
		out = append(out, gutter(i, downRows, maxV, true)+line)
	}
	out = append(out, spread(
		txStyle.Render("↑")+" "+bold.Render(humanRate(m.snap.TxBps)),
		muted.Render(fmt.Sprintf("peak %s · avg %s · session ↑ %s · pkts %s · drop %s",
			humanRate(maxV), humanRate(meanV), humanBytes(sess.tx), humanCount(sess.txPk), humanCount(sess.txDrop))),
		inner))
	return out
}

type sessionTotals struct {
	rx, tx, rxPk, txPk, rxDrop, txDrop uint64
}

func (m Model) session() sessionTotals {
	return sessionTotals{
		rx:     m.total.rx - m.base.rx,
		tx:     m.total.tx - m.base.tx,
		rxPk:   m.total.rxPk - m.base.rxPk,
		txPk:   m.total.txPk - m.base.txPk,
		rxDrop: m.total.rxDrop - m.base.rxDrop,
		txDrop: m.total.txDrop - m.base.txDrop,
	}
}

// gutter labels the y-axis: full scale at the outer edges, half scale at the middle of each half.
func gutter(i, n int, maxV float64, lowerHalf bool) string {
	label := ""
	switch {
	case !lowerHalf && i == 0:
		label = compact(maxV)
	case !lowerHalf && i == n/2:
		label = compact(maxV / 2)
	case lowerHalf && i == n/2:
		label = compact(maxV / 2)
	case lowerHalf && i == n-1:
		label = compact(maxV)
	}
	if label == "" {
		return strings.Repeat(" ", gutterW-1) + dim.Render("│")
	}
	return muted.Render(fmt.Sprintf("%*s", gutterW-2, label)) + dim.Render(" ┤")
}

func axisLine(cols int) string {
	const tag = " ┤ now ├"
	if cols < len(tag)+1 {
		return dim.Render(strings.Repeat("┈", cols))
	}
	return dim.Render(strings.Repeat("┈", cols-len(tag))) + muted.Render(" ┤ ") + bold.Render("now") + muted.Render(" ├")
}

func (m Model) activeCount() int {
	n := 0
	for _, i := range m.snap.Interfaces {
		if i.Up && i.RxTotal+i.TxTotal > 0 {
			n++
		}
	}
	return n
}

func (m Model) ifaceBody(inner, rows int) []string {
	const nameW, stateW, rateW, errW = 9, 2, 9, 3
	out := []string{muted.Render("  " + pad("IFACE", nameW) + " " + pad("ST", stateW) + " " +
		pad("DOWN", rateW) + " " + pad("UP", rateW) + " " + pad("ERR", errW) + " 60s")}
	hidden := 0
	for _, i := range m.snap.Interfaces {
		if !i.Up || i.RxTotal+i.TxTotal == 0 {
			hidden++
			continue
		}
		if len(out) >= rows-1 {
			hidden++
			continue
		}
		out = append(out, okStyle.Render("●")+" "+pad(trunc(i.Name, nameW), nameW)+" "+
			pad(okStyle.Render("up"), stateW)+" "+
			pad(rxStyle.Render(humanRate(i.RxBps)), rateW)+" "+
			pad(txStyle.Render(humanRate(i.TxBps)), rateW)+" "+
			pad(fmt.Sprintf("%d", i.Errors), errW)+" "+
			brailleSpark(m.ifHist[i.Name], 8))
	}
	if hidden > 0 {
		out = append(out, dim.Render(fmt.Sprintf("+%d idle or down hidden", hidden)))
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
	switch {
	case total == 0:
		return "no probes"
	case ok == total:
		return okStyle.Render("● ok")
	default:
		return badStyle.Render(fmt.Sprintf("● %d/%d ok", ok, total))
	}
}

func (m Model) healthBody(inner, rows int) []string {
	out := []string{muted.Render(fmt.Sprintf("  %-9s %-18s %-9s %s", "HOP", "TARGET", "RTT", "BUDGET"))}
	for _, t := range m.snap.Targets {
		hop := "target"
		if t.Kind == "gateway" {
			hop = "gateway"
		}
		out = append(out, healthRow(hop, t.Target, t.RTT, t.Err != ""))
	}
	for _, d := range m.snap.DNS {
		out = append(out, healthRow("dns", d.Server, d.RTT, d.Err != ""))
	}
	if len(out) == 1 {
		out = append(out, dim.Render("no gateway, resolvers, or targets · use -targets host:port"))
	}
	out = append(out, dim.Render(strings.Repeat("─", inner)))

	loss, jitter, probes := m.lossAndJitter()
	lossStr := okStyle.Render(fmt.Sprintf("%.1f%%", loss))
	if loss > 0 {
		lossStr = badStyle.Render(fmt.Sprintf("%.1f%%", loss))
	}
	out = append(out, fmt.Sprintf("%-13s %s    %-8s %s", "packet loss", lossStr, "jitter", fmtMS(jitter)))

	if m.snap.HasTCPCounters {
		secs := time.Since(m.startAt).Seconds()
		retrans := m.total.retrans - m.base.retrans
		ooo := m.total.ooo - m.base.ooo
		out = append(out, fmt.Sprintf("%-13s %s  %s  %s",
			"retrans", warnNum(retrans), dim.Render(fmt.Sprintf("(+%s/s)", perSecond(retrans, secs))),
			muted.Render(fmt.Sprintf("out of order %d", ooo))))
	}
	out = append(out, dim.Render(fmt.Sprintf("%d probes in window · %s", probes, fmtDuration(time.Duration(len(m.rxHist))*m.interval))))
	if len(out) > rows {
		out = out[:rows]
	}
	return out
}

func warnNum(n uint64) string {
	if n > 0 {
		return warn.Render(fmt.Sprintf("%d", n))
	}
	return okStyle.Render("0")
}

func perSecond(n uint64, secs float64) string {
	if secs <= 0 {
		return "0"
	}
	return fmt.Sprintf("%.1f", float64(n)/secs)
}

// healthRow: status dot, hop, target, RTT, and a 25-cell budget bar coloured by position.
func healthRow(hop, target string, rtt time.Duration, down bool) string {
	if down {
		return fmt.Sprintf("%s %-9s %-18s %-9s %s", badStyle.Render("●"), trunc(hop, 9), trunc(target, 18),
			badStyle.Render("timeout"), dim.Render(strings.Repeat("·", budgetCells)))
	}
	ms := float64(rtt.Microseconds()) / 1000
	filled := int(math.Ceil(math.Min(ms, budgetMaxMS) / budgetMaxMS * budgetCells))
	var bar strings.Builder
	for c := 0; c < budgetCells; c++ {
		if c >= filled {
			bar.WriteString(dim.Render("·"))
			continue
		}
		switch {
		case c < 6:
			bar.WriteString(okStyle.Render("■"))
		case c < 14:
			bar.WriteString(warn.Render("■"))
		default:
			bar.WriteString(badStyle.Render("■"))
		}
	}
	return fmt.Sprintf("%s %-9s %-18s %-9s %s", okStyle.Render("●"), trunc(hop, 9), trunc(target, 18),
		fmtMS(ms), bar.String())
}

// lossAndJitter reports loss across all target probes and mean RTT change between consecutive successes.
func (m Model) lossAndJitter() (loss, jitter float64, total int) {
	var fails int
	var deltaSum float64
	var deltaN int
	for _, series := range m.probes {
		var prev *collector.Probe
		for i := range series {
			p := series[i]
			total++
			if p.Err != "" {
				fails++
				prev = nil
				continue
			}
			if prev != nil {
				deltaSum += math.Abs(float64(p.RTT-prev.RTT) / float64(time.Millisecond))
				deltaN++
			}
			prev = &series[i]
		}
	}
	if total > 0 {
		loss = float64(fails) / float64(total) * 100
	}
	if deltaN > 0 {
		jitter = deltaSum / float64(deltaN)
	}
	return loss, jitter, total
}

// connLayout decides which columns fit the panel width, dropping the least useful first.
type connLayout struct {
	remoteW            int
	port, proto, spark bool
}

func layoutConns(inner int) connLayout {
	l := connLayout{port: true, proto: true, spark: true}
	for {
		fixed := 2 + 16 + 7 + 9 + 9 + 7 + 6 // marker, process, pid, down, up, rtt, state
		cols := 7
		if l.port {
			fixed += 6
			cols++
		}
		if l.proto {
			fixed += 5
			cols++
		}
		if l.spark {
			fixed += 12
			cols++
		}
		l.remoteW = inner - fixed - cols
		if l.remoteW >= 22 || (!l.spark && !l.proto && !l.port) {
			break
		}
		switch {
		case l.spark:
			l.spark = false
		case l.proto:
			l.proto = false
		default:
			l.port = false
		}
	}
	if l.remoteW < 10 {
		l.remoteW = 10
	}
	return l
}

func (l connLayout) row(marker, proc, pid, remote, port, proto, down, up, rtt, state, spark string) string {
	cells := []string{pad(marker, 2), pad(proc, 16), pad(pid, 7), pad(remote, l.remoteW)}
	if l.port {
		cells = append(cells, pad(port, 6))
	}
	if l.proto {
		cells = append(cells, pad(proto, 5))
	}
	cells = append(cells, pad(down, 9), pad(up, 9), pad(rtt, 7), pad(state, 6))
	if l.spark {
		cells = append(cells, pad(spark, 12))
	}
	return strings.Join(cells, " ")
}

// connBody: the selected process is detailed on top; the process table is below it, busiest first.
func (m Model) connBody(inner, rows int) []string {
	groups := groupsOf(m.visibleConns())
	if len(groups) == 0 {
		return []string{dim.Render("no established tcp connections (or insufficient privilege)")}
	}
	l := layoutConns(inner)
	sel := m.selIndex(groups)
	g := groups[sel]

	var out []string
	out = append(out, accentB.Render("↳ ")+bold.Render(trunc(g.name, 30))+
		dim.Render("  pid "+pidList(g.pids)))
	out = append(out, muted.Render(fmt.Sprintf("%s to %s", plural(len(g.conns), "connection"), plural(len(distinctHosts(g.conns)), "host"))))
	out = append(out, muted.Render(trunc(strings.Join(distinctHosts(g.conns), "  "), inner)))
	out = append(out, muted.Render(l.row("", "PROCESS", "PID", "REMOTE", "PORT", "PROTO", "DOWN", "UP", "RTT", "STATE", "60s")))

	for gi, grp := range groups {
		if len(out) >= rows-1 {
			out = append(out, dim.Render(fmt.Sprintf("+%d more processes (sorted by traffic)", len(groups)-gi)))
			break
		}
		marker := " "
		if gi == sel {
			marker = accentB.Render("▌")
		}
		fold := "▶"
		if m.expanded[grp.name] {
			fold = "▼"
		}
		pidCol := fmt.Sprintf("%d", grp.pids[0])
		if len(grp.pids) > 1 {
			pidCol = fmt.Sprintf("%d×", len(grp.pids))
		}
		out = append(out, l.row(marker+fold,
			bold.Render(trunc(grp.name, 16)), pidCol,
			dim.Render(fmt.Sprintf("%s · %s", plural(len(grp.conns), "conn"), plural(len(distinctHosts(grp.conns)), "host"))),
			"", "",
			rateCell(grp.rx, grp.hasIO, rxStyle), rateCell(grp.tx, grp.hasIO, txStyle),
			rttCell(grp.rtt, grp.hasRT), stateLabel(grp.conns),
			brailleSpark(m.procHist[grp.name], 12)))
		if !m.expanded[grp.name] {
			continue
		}
		for _, c := range grp.conns {
			if len(out) >= rows {
				break
			}
			host, port := splitRemote(c.Remote)
			proto := "tcp"
			if c.State == "UDP" {
				proto = "udp"
			}
			out = append(out, l.row("  ", "", fmt.Sprintf("%d", c.PID),
				trunc(host, l.remoteW), trunc(port, 6), proto,
				rateCell(c.RxBps, c.HasIO, rxStyle), rateCell(c.TxBps, c.HasIO, txStyle),
				rttCell(c.RTTms, c.RTTms >= 0), stateCell(c.State),
				brailleSpark(m.sockHist[c.Local+">"+c.Remote], 12)))
		}
	}
	if len(out) > rows {
		out = out[:rows]
	}
	return out
}

// stateLabel summarises a process's sockets: one state, or "mixed".
func stateLabel(conns []collector.Conn) string {
	first := conns[0].State
	for _, c := range conns[1:] {
		if c.State != first {
			return dim.Render("mixed")
		}
	}
	return stateCell(first)
}

func stateCell(state string) string {
	switch state {
	case "ESTABLISHED":
		return okStyle.Render("ESTAB")
	case "UDP":
		return accentB.Render("UDP")
	}
	return muted.Render(state)
}

func rateCell(bps float64, known bool, style lipgloss.Style) string {
	if !known {
		return dim.Render("--")
	}
	return style.Render(humanRate(bps))
}

func rttCell(ms float64, known bool) string {
	if !known {
		return dim.Render("--")
	}
	return fmtMS(ms)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func pidList(pids []int32) string {
	var parts []string
	for i, p := range pids {
		if i == 4 {
			parts = append(parts, fmt.Sprintf("+%d", len(pids)-4))
			break
		}
		parts = append(parts, fmt.Sprintf("%d", p))
	}
	return strings.Join(parts, ",")
}

func distinctHosts(conns []collector.Conn) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range conns {
		h := hostOf(c.Remote)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// pad fits a possibly styled string into exactly w visible columns.
func pad(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = lipgloss.NewStyle().MaxWidth(w).Render(s)
	}
	if n := w - lipgloss.Width(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
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

	for i := 0; i < height-2; i++ {
		content := ""
		if i < len(body) {
			content = lipgloss.NewStyle().MaxWidth(inner).Render(body[i])
		}
		out = append(out, border.Render("│ ")+pad(content, inner)+border.Render(" │"))
	}

	foot := border.Render("╰─")
	if keys != "" {
		foot = border.Render("╰─┤ ") + keyLine(keys) + border.Render(" ├")
	}
	fillB := width - lipgloss.Width(foot) - 1
	if fillB < 0 {
		fillB = 0
	}
	return append(out, foot+border.Render(strings.Repeat("─", fillB))+border.Render("╯"))
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

// joinH places panels side by side; blocks are padded to equal height.
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

// graph renders mirrored braille halves: rx grows up from the zero line, tx grows down.
func graph(rxS, txS []float64, cols, upRows, downRows int) (upper, lower []string) {
	samples := cols * 2
	rxT := tail(rxS, samples)
	txT := tail(txS, samples)
	maxV := peak(rxS, txS)
	if maxV <= 0 {
		maxV = 1
	}

	rows := upRows + downRows
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

	for r := 0; r < rows; r++ {
		var sb strings.Builder
		for c := 0; c < cols; c++ {
			sb.WriteRune(rune(0x2800 + int(bits[r][c]))) // #nosec G115 -- uint8 offset stays in the Braille block
		}
		if r < upRows {
			upper = append(upper, rxStyle.Render(sb.String()))
		} else {
			lower = append(lower, txStyle.Render(sb.String()))
		}
	}
	return upper, lower
}

// brailleSpark draws two samples per cell as dots rising from the bottom row.
func brailleSpark(h []float64, width int) string {
	vals := tail(h, width*2)
	maxV := 0.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	dotBit := [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}
	var sb strings.Builder
	for c := 0; c < width; c++ {
		var bits uint8
		for s := 0; s < 2; s++ {
			lvl := levelOf(vals[c*2+s], maxV, 4)
			for k := 0; k < lvl; k++ {
				bits |= dotBit[s][3-k]
			}
		}
		sb.WriteRune(rune(0x2800 + int(bits)))
	}
	return dim.Render(sb.String())
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

func fmtDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	}
	if s >= 60 {
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
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

func humanCount(n uint64) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return fmt.Sprintf("%.1fG", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.1fM", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1fK", f/1e3)
	}
	return fmt.Sprintf("%d", n)
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

func (m Model) packetsPanel(w, h int) []string {
	meta := "off"
	if m.snap.Capture.Enabled {
		meta = fmt.Sprintf("%s pps", humanCount(uint64(m.snap.Capture.PPS)))
	}
	return panel("5", "packets", meta, w, h, m.packetsBody(w-4, h-2), "q quit")
}

func (m Model) packetsBody(inner, rows int) []string {
	c := m.snap.Capture
	if !c.Enabled {
		msg := c.Error
		if msg == "" {
			msg = "capture is off (-capture=false)"
		}
		return []string{dim.Render("packet capture unavailable: " + msg)}
	}

	var out []string
	out = append(out, muted.Render(fmt.Sprintf("%s · %s · %s packets seen",
		humanRate(c.BPS), fmt.Sprintf("%.0f pps", c.PPS), humanCount(c.Packets))))
	out = append(out, muted.Render(protoSummary(c.Protocols)))

	half := inner / 2
	out = append(out, pad(bold.Render("DNS QUERIES"), half)+bold.Render("TLS SERVER NAMES"))
	dns, sni := c.DNS, c.SNI
	for i := 0; i < 8; i++ {
		left, right := "", ""
		if i < len(dns) {
			left = fmt.Sprintf("%s %s", trunc(dns[i].Name, half-8), dim.Render(fmt.Sprintf("%d", dns[i].Count)))
		}
		if i < len(sni) {
			right = fmt.Sprintf("%s %s", trunc(sni[i].Name, inner-half-8), dim.Render(fmt.Sprintf("%d", sni[i].Count)))
		}
		out = append(out, pad(left, half)+right)
	}

	out = append(out, muted.Render("TOP FLOWS"))
	for _, f := range c.Flows {
		if len(out) >= rows-6 {
			break
		}
		out = append(out, fmt.Sprintf("%s %s → %s %s",
			pad(f.Proto, 5), pad(trunc(f.Src, 24), 24), pad(trunc(f.Dst, 24), 24), dim.Render(humanBytes(f.Bytes))))
	}

	out = append(out, muted.Render("RECENT"))
	for i := len(c.Recent) - 1; i >= 0 && len(out) < rows; i-- {
		p := c.Recent[i]
		label := p.DNSName + " " + p.DNSType + p.SNI
		out = append(out, fmt.Sprintf("%s %s → %s %s",
			pad(p.Proto, 5), pad(trunc(p.Src, 24), 24), pad(trunc(p.Dst, 24), 24), dim.Render(trunc(label, inner-60))))
	}
	if len(out) > rows {
		out = out[:rows]
	}
	return out
}

func protoSummary(p map[string]uint64) string {
	var total uint64
	for _, n := range p {
		total += n
	}
	if total == 0 {
		return "no packets yet"
	}
	order := []string{"tcp", "udp", "icmp", "icmpv6", "other"}
	var parts []string
	for _, k := range order {
		if n := p[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", k, float64(n)/float64(total)*100))
		}
	}
	return strings.Join(parts, "  ")
}
