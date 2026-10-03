package collector

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"strconv"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// netstatConnections reads the kernel socket table via `netstat -anv`, which
// reports PIDs for sockets that `lsof` cannot see without root.
func netstatConnections() ([]gnet.ConnectionStat, error) {
	raw, err := exec.Command("netstat", "-anv", "-p", "tcp").Output()
	if err != nil {
		return nil, err
	}
	var out []gnet.ConnectionStat
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		if c, ok := parseNetstatRow(sc.Text()); ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// parseNetstatRow handles rows like:
//
//	tcp4  0  0  192.168.2.225.52914  17.57.144.120.5223  ESTABLISHED  ...  apsd:147  ...
func parseNetstatRow(line string) (gnet.ConnectionStat, bool) {
	f := strings.Fields(line)
	if len(f) < 6 || !strings.HasPrefix(f[0], "tcp") {
		return gnet.ConnectionStat{}, false
	}
	laddr, lport, ok := splitNetstatAddr(f[3])
	if !ok {
		return gnet.ConnectionStat{}, false
	}
	raddr, rport, ok := splitNetstatAddr(f[4])
	if !ok {
		return gnet.ConnectionStat{}, false
	}
	c := gnet.ConnectionStat{
		Status: f[5],
		Laddr:  gnet.Addr{IP: laddr, Port: lport},
		Raddr:  gnet.Addr{IP: raddr, Port: rport},
	}
	for _, field := range f[6:] {
		if i := strings.LastIndex(field, ":"); i > 0 {
			if pid, err := strconv.ParseInt(field[i+1:], 10, 32); err == nil {
				c.Pid = int32(pid)
				break
			}
		}
	}
	return c, true
}

// splitNetstatAddr splits "192.168.2.225.52914" or "fe80::1.1024" on the last dot.
func splitNetstatAddr(s string) (string, uint32, bool) {
	i := strings.LastIndex(s, ".")
	if i <= 0 {
		return "", 0, false
	}
	port, err := strconv.ParseUint(s[i+1:], 10, 16)
	if err != nil {
		return "", 0, false
	}
	ip := s[:i]
	if net.ParseIP(ip) == nil {
		return "", 0, false
	}
	return ip, uint32(port), true
}
