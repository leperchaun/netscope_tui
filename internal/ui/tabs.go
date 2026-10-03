package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var tabNames = [...]string{"dashboard", "connections", "interfaces", "packets", "stats", "topology", "timeline", "processes", "diagnose", "egress"}

func (m Model) tabBar(w int) string {
	var chips []string
	if m.rec != nil {
		chips = append(chips, badStyle.Render("● rec"))
	}
	if m.frozen {
		chips = append(chips, warn.Render("frozen"))
	}
	if m.paused {
		chips = append(chips, warn.Render("paused"))
	}
	if m.filter != "" || m.filtering {
		chips = append(chips, accentB.Render("/"+m.filter))
	}
	right := strings.Join(chips, "  ")

	// Full names when they fit; otherwise shorten from the right, keeping every digit.
	for _, n := range []int{99, 4, 3} {
		var b strings.Builder
		b.WriteString(brandStyleFn().Render("◉ netscope") + " ")
		for i, name := range tabNames {
			key := (i + 1) % 10
			label := name
			if n < len(name) {
				label = name[:n]
			}
			if i == m.tab {
				b.WriteString(accentB.Render(fmt.Sprintf("%d %s", key, label)) + " ")
			} else {
				b.WriteString(dim.Render(fmt.Sprintf("%d", key)) + " " + muted.Render(label) + " ")
			}
		}
		line := b.String()
		if lipgloss.Width(line)+lipgloss.Width(right)+1 <= w || n == 3 {
			return spread(line, right, w)
		}
	}
	return ""
}

func brandStyleFn() lipgloss.Style { return accentB }

func (m Model) body(w, h int) []string {
	switch m.tab {
	case 1:
		return m.connectionsTab(w, h)
	case 2:
		return m.interfacesTab(w, h)
	case 3:
		return m.packetsPanel(w, h)
	case 4:
		return m.statsTab(w, h)
	case 5:
		return m.topologyTab(w, h)
	case 6:
		return m.timelineTab(w, h)
	case 7:
		return m.processesTab(w, h)
	case 8:
		return m.notBuilt(w, h, "diagnose", "needs rule definitions for the issue engine: which baselines, thresholds, and causes to report. The sample's rules are not reproduced here.")
	case 9:
		return m.notBuilt(w, h, "egress", "needs a destination learning store and a policy model: which destinations to allow, how drift is measured, and where the policy is kept.")
	}
	return nil
}

func (m Model) connectionsTab(w, h int) []string {
	groups := groupsOf(m.visibleConns())
	meta := fmt.Sprintf("%d in %d procs", len(m.visibleConns()), len(groups))
	if m.filter != "" {
		meta += " · filter " + m.filter
	}
	return panel("2", "connections", meta, w, h, m.connBody(w-4, h-2),
		"↑↓ select  space fold  z fold all  / filter  esc clear  q quit")
}

func (m Model) interfacesTab(w, h int) []string {
	inner := w - 4
	const nameW = 12
	rows := []string{muted.Render(fmt.Sprintf("%-*s %-6s %-12s %-12s %-6s %-12s %-12s", nameW, "IFACE", "STATE", "DOWN", "UP", "ERR", "RX TOTAL", "TX TOTAL"))}
	busiest := ""
	var best float64 = -1
	for _, i := range m.snap.Interfaces {
		state := badStyle.Render("down")
		if i.Up {
			state = okStyle.Render("up")
		}
		if i.Up && i.RxBps+i.TxBps > best && !isLoopback(i.Name) {
			best, busiest = i.RxBps+i.TxBps, i.Name
		}
		rows = append(rows, fmt.Sprintf("%-*s %s %s %s %-6d %-12s %-12s",
			nameW, trunc(i.Name, nameW), pad(state, 6),
			pad(rxStyle.Render(humanRate(i.RxBps)), 12), pad(txStyle.Render(humanRate(i.TxBps)), 12),
			i.Errors, humanBytes(i.RxTotal), humanBytes(i.TxTotal)))
	}
	rows = append(rows, "")
	if busiest != "" {
		rows = append(rows, accentB.Render("busiest · "+busiest))
		up := max(2, (h-2-len(rows)-2)/2)
		upper, lower := graph(m.ifHist[busiest], nil, inner-gutterW, up, max(2, (h-2-len(rows)-2)-up))
		for _, line := range append(upper, lower...) {
			rows = append(rows, strings.Repeat(" ", gutterW)+line)
		}
	}
	if len(rows) > h-2 {
		rows = rows[:h-2]
	}
	return panel("3", "interfaces", fmt.Sprintf("%d total", len(m.snap.Interfaces)), w, h, rows, "q quit")
}

func (m Model) statsTab(w, h int) []string {
	inner := w - 4
	c := m.snap.Capture
	var rows []string
	rows = append(rows, bold.Render("protocols (capture)"))
	var total uint64
	for _, n := range c.Protocols {
		total += n
	}
	for _, p := range []string{"tcp", "udp", "icmp", "icmpv6", "other"} {
		n := c.Protocols[p]
		pct := 0.0
		if total > 0 {
			pct = float64(n) / float64(total) * 100
		}
		rows = append(rows, fmt.Sprintf("%-7s %s %5.1f%%  %s", p, bar(pct, 40), pct, dim.Render(humanCount(n))))
	}
	rows = append(rows, "")
	rows = append(rows, bold.Render("tcp states"))
	var max int
	for _, n := range m.snap.TCPStates {
		if n > max {
			max = n
		}
	}
	for _, st := range []string{"ESTABLISHED", "LISTEN", "TIME_WAIT", "CLOSE_WAIT", "SYN_SENT", "SYN_RECV", "FIN_WAIT1", "FIN_WAIT2", "LAST_ACK", "CLOSING"} {
		if n := m.snap.TCPStates[st]; n > 0 {
			rows = append(rows, fmt.Sprintf("%-12s %s %d", st, bar(float64(n)/float64(max)*100, 40), n))
		}
	}
	rows = append(rows, "")
	sess := m.session()
	rows = append(rows, bold.Render("counters (session)"))
	rows = append(rows, fmt.Sprintf("packets in %s  out %s   drops in %s  out %s",
		humanCount(sess.rxPk), humanCount(sess.txPk), humanCount(sess.rxDrop), humanCount(sess.txDrop)))
	if m.snap.HasTCPCounters {
		rows = append(rows, fmt.Sprintf("retransmitted segments %d   out of order %d",
			m.total.retrans-m.base.retrans, m.total.ooo-m.base.ooo))
	}
	rows = append(rows, "")
	rows = append(rows, bold.Render("top DNS queries"))
	for i, n := range c.DNS {
		if i >= 5 {
			break
		}
		rows = append(rows, fmt.Sprintf("%s %d", trunc(n.Name, inner-8), n.Count))
	}
	rows = append(rows, bold.Render("top TLS server names"))
	for i, n := range c.SNI {
		if i >= 5 {
			break
		}
		rows = append(rows, fmt.Sprintf("%s %d", trunc(n.Name, inner-8), n.Count))
	}
	if len(rows) > h-2 {
		rows = rows[:h-2]
	}
	return panel("5", "stats", "", w, h, rows, "q quit")
}

// bar draws a percentage as filled blocks; width is the full scale in cells.
func bar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return rxStyle.Render(strings.Repeat("■", filled)) + dim.Render(strings.Repeat("·", width-filled))
}

func (m Model) topologyTab(w, h int) []string {
	var rows []string
	rows = append(rows, accentB.Render("● "+m.host)+dim.Render("  this machine"))
	hop := func(label, target string, rtt string, down bool) {
		status := okStyle.Render("●")
		if down {
			status = badStyle.Render("●")
		}
		rows = append(rows, dim.Render("│"))
		rows = append(rows, fmt.Sprintf("%s %-9s %-24s %s", status, label, trunc(target, 24), rtt))
	}
	for _, t := range m.snap.Targets {
		label := "target"
		if t.Kind == "gateway" {
			label = "gateway"
		}
		rtt := fmtMS(float64(t.RTT.Microseconds()) / 1000)
		hop(label, t.Target, rtt, t.Err != "")
	}
	for _, d := range m.snap.DNS {
		rtt := fmtMS(float64(d.RTT.Microseconds()) / 1000)
		hop("dns", d.Server, rtt, d.Err != "")
	}
	rows = append(rows, "")
	rows = append(rows, muted.Render("top remote hosts by traffic"))
	type hostRate struct {
		host string
		bps  float64
	}
	byHost := map[string]float64{}
	for _, c := range m.snap.Conns {
		byHost[hostOf(c.Remote)] += c.RxBps + c.TxBps
	}
	var list []hostRate
	for h, v := range byHost {
		list = append(list, hostRate{h, v})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].bps > list[i].bps {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	for i, hr := range list {
		if i >= 10 {
			break
		}
		rows = append(rows, fmt.Sprintf("  %-40s %s", trunc(hr.host, 40), humanRate(hr.bps)))
	}
	if len(rows) > h-2 {
		rows = rows[:h-2]
	}
	return panel("6", "topology", "", w, h, rows, "q quit")
}

func (m Model) timelineTab(w, h int) []string {
	inner := w - 4
	label := 14
	cells := inner - label
	series := []struct {
		name string
		hist []float64
		unit string
	}{
		{"download", m.rxHist, humanRate(m.snap.RxBps)},
		{"upload", m.txHist, humanRate(m.snap.TxBps)},
		{"connections", m.connHist, fmt.Sprintf("%d", len(m.snap.Conns))},
		{"retransmits", deltas(m.retransHist), ""},
		{"capture pps", m.ppsHist, fmt.Sprintf("%.0f", m.snap.Capture.PPS)},
	}
	var rows []string
	for _, s := range series {
		rows = append(rows, fmt.Sprintf("%-*s %s %s", label, s.name, brailleSpark(s.hist, cells), dim.Render(s.unit)))
	}
	rows = append(rows, "")
	rows = append(rows, muted.Render("events (probe up/down)"))
	start := len(m.events) - (h - 2 - len(rows))
	if start < 0 {
		start = 0
	}
	rows = append(rows, m.events[start:]...)
	if len(m.events) == 0 {
		rows = append(rows, dim.Render("no changes yet"))
	}
	if len(rows) > h-2 {
		rows = rows[:h-2]
	}
	return panel("7", "timeline", fmt.Sprintf("%d samples", len(m.rxHist)), w, h, rows, "f freeze  r record  q quit")
}

// deltas turns cumulative counter samples into per-sample increments.
func deltas(h []float64) []float64 {
	out := make([]float64, 0, len(h))
	for i := 1; i < len(h); i++ {
		d := h[i] - h[i-1]
		if d < 0 {
			d = 0
		}
		out = append(out, d)
	}
	return out
}

func (m Model) processesTab(w, h int) []string {
	groups := groupsOf(m.visibleConns())
	const procW = 22
	rows := []string{muted.Render(fmt.Sprintf("%-*s %-8s %-6s %-6s %-12s %-12s %-9s %s", procW, "PROCESS", "PIDS", "CONNS", "HOSTS", "DOWN", "UP", "RTT", "60s"))}
	for gi, g := range groups {
		if len(rows) >= h-2 {
			break
		}
		marker := " "
		if gi == 0 {
			marker = accentB.Render("▌")
		}
		rows = append(rows, fmt.Sprintf("%s%-*s %-8d %-6d %-6d %s %s %s %s",
			marker, procW-1, trunc(g.name, procW-1), len(g.pids), len(g.conns), len(distinctHosts(g.conns)),
			pad(rateCell(g.rx, g.hasIO, rxStyle), 12), pad(rateCell(g.tx, g.hasIO, txStyle), 12),
			pad(rttCell(g.rtt, g.hasRT), 9), brailleSpark(m.procHist[g.name], 12)))
	}
	return panel("8", "processes", fmt.Sprintf("%d processes", len(groups)), w, h, rows, "↑↓ select  q quit")
}

func (m Model) notBuilt(w, h int, name, need string) []string {
	body := []string{
		warn.Render("not built yet"),
		"",
		muted.Render(need),
	}
	return panel(map[string]string{"diagnose": "9", "egress": "0"}[name], name, "", w, h, body, "q quit")
}
