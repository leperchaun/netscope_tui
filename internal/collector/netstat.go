package collector

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type socketRow struct {
	PID        int32
	LocalIP    string
	LocalPort  uint32
	RemoteIP   string
	RemotePort uint32
	State      string
	Rx, Tx     uint64
	HasIO      bool
	RTTms      float64
}

func (r socketRow) key() string {
	return connKey(r.LocalIP, r.LocalPort, r.RemoteIP, r.RemotePort)
}

func connKey(lip string, lport uint32, rip string, rport uint32) string {
	return net.JoinHostPort(lip, strconv.FormatUint(uint64(lport), 10)) + ">" +
		net.JoinHostPort(rip, strconv.FormatUint(uint64(rport), 10))
}

// netstatRows reads per-socket byte counters and PIDs from the kernel socket table.
// Unlike lsof, it sees sockets owned by system daemons without root.
func netstatRows() ([]socketRow, error) {
	return netstatProto("tcp", parseNetstatRow)
}

// netstatUDPRows returns connected UDP sockets (QUIC and DNS flows), which carry no TCP state.
func netstatUDPRows() ([]socketRow, error) {
	return netstatProto("udp", parseNetstatUDPRow)
}

func netstatProto(proto string, parse func(string) (socketRow, bool)) ([]socketRow, error) {
	raw, err := exec.Command("netstat", "-anv", "-p", proto).Output()
	if err != nil {
		return nil, err
	}
	var out []socketRow
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		if r, ok := parse(sc.Text()); ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// parseNetstatUDPRow handles connected UDP sockets. Unconnected sockets (foreign "*.*") are skipped.
// UDP rows have no state column, so the byte counters come straight after the foreign address.
func parseNetstatUDPRow(line string) (socketRow, bool) {
	f := strings.Fields(line)
	if len(f) < 6 || !strings.HasPrefix(f[0], "udp") {
		return socketRow{}, false
	}
	if f[4] == "*.*" {
		return socketRow{}, false
	}
	lip, lport, ok := splitNetstatAddr(f[3])
	if !ok {
		return socketRow{}, false
	}
	rip, rport, ok := splitNetstatAddr(f[4])
	if !ok {
		return socketRow{}, false
	}
	r := socketRow{LocalIP: lip, LocalPort: lport, RemoteIP: rip, RemotePort: rport, State: "UDP", RTTms: -1}
	start := 5
	if _, err := strconv.ParseUint(f[5], 10, 64); err != nil && len(f) > 6 {
		start = 6
	}
	if len(f) >= start+2 {
		rx, errR := strconv.ParseUint(f[start], 10, 64)
		tx, errT := strconv.ParseUint(f[start+1], 10, 64)
		if errR == nil && errT == nil {
			r.Rx, r.Tx, r.HasIO = rx, tx, true
		}
	}
	for _, field := range f[start:] {
		if i := strings.LastIndex(field, ":"); i > 0 {
			if pid, err := strconv.ParseInt(field[i+1:], 10, 32); err == nil {
				r.PID = int32(pid)
				break
			}
		}
	}
	return r, true
}

// parseNetstatRow handles rows like:
//
//	tcp4  0  0  192.168.2.225.52914  17.57.144.120.5223  ESTABLISHED  153620  460120  ...  apsd:147  ...
func parseNetstatRow(line string) (socketRow, bool) {
	f := strings.Fields(line)
	if len(f) < 6 || !strings.HasPrefix(f[0], "tcp") {
		return socketRow{}, false
	}
	lip, lport, ok := splitNetstatAddr(f[3])
	if !ok {
		return socketRow{}, false
	}
	rip, rport, ok := splitNetstatAddr(f[4])
	if !ok {
		return socketRow{}, false
	}
	r := socketRow{LocalIP: lip, LocalPort: lport, RemoteIP: rip, RemotePort: rport, State: f[5], RTTms: -1}
	if len(f) >= 8 {
		rx, errR := strconv.ParseUint(f[6], 10, 64)
		tx, errT := strconv.ParseUint(f[7], 10, 64)
		if errR == nil && errT == nil {
			r.Rx, r.Tx, r.HasIO = rx, tx, true
		}
	}
	for _, field := range f[6:] {
		if i := strings.LastIndex(field, ":"); i > 0 {
			if pid, err := strconv.ParseInt(field[i+1:], 10, 32); err == nil {
				r.PID = int32(pid)
				break
			}
		}
	}
	return r, true
}

// splitNetstatAddr splits "192.168.2.225.52914" or "fe80::1.1024" on the last dot.
// netstat abbreviates long IPv6 addresses ("fe80::896:56ab:c.52924"), so the address is kept as printed.
// Wildcards ("*.22", "*.*") are kept so listening sockets still count toward TCP states.
func splitNetstatAddr(s string) (string, uint32, bool) {
	i := strings.LastIndex(s, ".")
	if i <= 0 {
		return "", 0, false
	}
	ip := s[:i]
	var port uint32
	if p := s[i+1:]; p != "*" {
		v, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return "", 0, false
		}
		port = uint32(v)
	}
	return ip, port, true
}

// parseNetstatStats extracts cumulative retransmitted and out-of-order TCP packet counts
// from `netstat -s -p tcp`.
func parseNetstatStats(text string) (retrans, outOfOrder uint64, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(line, "data packet") && strings.Contains(line, "retransmitted"):
			retrans, ok = n, true
		case strings.Contains(line, "out-of-order packet"):
			outOfOrder, ok = n, true
		}
	}
	return retrans, outOfOrder, ok
}
