package collector

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const dnsTimeout = 2 * time.Second

type DNSProbe struct {
	Server string        `json:"server"`
	Query  string        `json:"query"`
	RTT    time.Duration `json:"rtt_ns"`
	Rcode  string        `json:"rcode,omitempty"`
	Err    string        `json:"error,omitempty"`
}

// SystemResolvers returns the DNS servers the OS is currently configured to use.
func SystemResolvers() []string {
	var servers []string
	if runtime.GOOS == "windows" {
		servers = windowsResolvers()
	} else {
		servers = unixResolvers("/etc/resolv.conf")
	}
	return uniq(servers)
}

func unixResolvers(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[0] == "nameserver" {
			out = append(out, fields[1])
		}
	}
	return out
}

func windowsResolvers() []string {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-DnsClientServerAddress -AddressFamily IPv4 | ForEach-Object { $_.ServerAddresses }) -join \"`n\"")
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if ip := net.ParseIP(strings.TrimSpace(line)); ip != nil {
			out = append(out, ip.String())
		}
	}
	return out
}

func ProbeDNS(server, name string) DNSProbe {
	p := DNSProbe{Server: server, Query: name}
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.TypeA)
	c := &dns.Client{Timeout: dnsTimeout}
	start := time.Now()
	resp, _, err := c.Exchange(msg, net.JoinHostPort(server, "53"))
	p.RTT = time.Since(start)
	if err != nil {
		p.Err = err.Error()
		return p
	}
	p.Rcode = dns.RcodeToString[resp.Rcode]
	return p
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
