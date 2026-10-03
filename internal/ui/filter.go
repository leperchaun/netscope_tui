package ui

import (
	"strings"

	"netscope/internal/collector"
)

// visibleConns applies the filter text to sockets (process, local, remote, state).
func (m Model) visibleConns() []collector.Conn {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	if q == "" {
		return m.snap.Conns
	}
	var out []collector.Conn
	for _, c := range m.snap.Conns {
		hay := strings.ToLower(c.Process + " " + c.Local + " " + c.Remote + " " + c.State)
		if strings.Contains(hay, q) {
			out = append(out, c)
		}
	}
	return out
}
