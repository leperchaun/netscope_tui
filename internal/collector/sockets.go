package collector

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// socketRows returns every TCP socket with the best data this OS exposes.
// macOS: netstat (byte counters, PIDs). Linux: gopsutil plus ss for bytes and RTT.
// Elsewhere: gopsutil only, which has no per-socket byte counters.
func socketRows() []socketRow {
	switch runtime.GOOS {
	case "darwin":
		if rows, err := netstatRows(); err == nil && len(rows) > 0 {
			return rows
		}
		return gopsutilRows()
	case "linux":
		rows := gopsutilRows()
		mergeSS(rows)
		return rows
	default:
		return gopsutilRows()
	}
}

func gopsutilRows() []socketRow {
	conns, err := gnet.Connections("tcp")
	if err != nil {
		return nil
	}
	rows := make([]socketRow, 0, len(conns))
	for _, c := range conns {
		rows = append(rows, socketRow{
			PID:        c.Pid,
			LocalIP:    c.Laddr.IP,
			LocalPort:  c.Laddr.Port,
			RemoteIP:   c.Raddr.IP,
			RemotePort: c.Raddr.Port,
			State:      c.Status,
			RTTms:      -1,
		})
	}
	return rows
}

type ssEntry struct {
	rx, tx uint64
	rtt    float64
}

func mergeSS(rows []socketRow) {
	info := ssInfo()
	for i := range rows {
		if e, ok := info[rows[i].key()]; ok {
			rows[i].Rx, rows[i].Tx, rows[i].RTTms, rows[i].HasIO = e.rx, e.tx, e.rtt, true
		}
	}
}

// ssInfo parses `ss -tinHn`: each socket line is followed by an indented info line
// carrying rtt:<ms>/<var> and bytes_received / bytes_sent.
func ssInfo() map[string]ssEntry {
	raw, err := exec.Command("ss", "-tinHn").Output()
	if err != nil {
		return map[string]ssEntry{}
	}
	return parseSS(string(raw))
}

func parseSS(text string) map[string]ssEntry {
	out := map[string]ssEntry{}
	var current string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if current == "" {
				continue
			}
			e := out[current]
			e.rtt = -1
			for _, tok := range strings.Fields(line) {
				switch {
				case strings.HasPrefix(tok, "rtt:"):
					v := strings.TrimPrefix(tok, "rtt:")
					if i := strings.Index(v, "/"); i >= 0 {
						v = v[:i]
					}
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						e.rtt = f
					}
				case strings.HasPrefix(tok, "bytes_received:"):
					e.rx, _ = strconv.ParseUint(strings.TrimPrefix(tok, "bytes_received:"), 10, 64)
				case strings.HasPrefix(tok, "bytes_sent:"):
					e.tx, _ = strconv.ParseUint(strings.TrimPrefix(tok, "bytes_sent:"), 10, 64)
				}
			}
			out[current] = e
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 || f[0] == "State" {
			current = ""
			continue
		}
		current = ssKey(f[3], f[4])
		out[current] = ssEntry{rtt: -1}
	}
	return out
}

// ssKey normalises "[fe80::1%en0]:443" and "1.2.3.4:443" to match socketRow.key().
func ssKey(local, remote string) string {
	return normalizeAddr(local) + ">" + normalizeAddr(remote)
}

func normalizeAddr(a string) string {
	i := strings.LastIndex(a, ":")
	if i < 0 {
		return a
	}
	ip := strings.Trim(a[:i], "[]")
	if j := strings.Index(ip, "%"); j >= 0 {
		ip = ip[:j]
	}
	return net.JoinHostPort(ip, a[i+1:])
}

// tcpCounters returns cumulative retransmitted and out-of-order TCP segments.
func tcpCounters() (retrans, outOfOrder uint64, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		raw, err := exec.Command("netstat", "-s", "-p", "tcp").Output()
		if err != nil {
			return 0, 0, false
		}
		return parseNetstatStats(string(raw))
	case "linux":
		return linuxTCPCounters()
	}
	return 0, 0, false
}

func linuxTCPCounters() (retrans, outOfOrder uint64, ok bool) {
	snmp, err := os.ReadFile("/proc/net/snmp")
	if err != nil {
		return 0, 0, false
	}
	ext, err := os.ReadFile("/proc/net/netstat")
	if err != nil {
		return 0, 0, false
	}
	r, okR := keyedValue(string(snmp), "Tcp:", "RetransSegs")
	o, okO := keyedValue(string(ext), "TcpExt:", "TCPOFOQueue")
	return r, o, okR && okO
}

// keyedValue reads a named counter from a two-line /proc table (header line, then values line).
func keyedValue(text, prefix, name string) (uint64, bool) {
	var header []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		f := strings.Fields(line)
		if header == nil {
			header = f
			continue
		}
		for i, h := range header {
			if h == name && i < len(f) {
				v, err := strconv.ParseUint(f[i], 10, 64)
				return v, err == nil
			}
		}
	}
	return 0, false
}
