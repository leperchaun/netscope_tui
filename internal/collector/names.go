package collector

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const reverseLookupTimeout = 2 * time.Second

var wellKnownPorts = map[uint16]string{
	22: "ssh", 53: "domain", 80: "http", 143: "imap", 443: "https",
	465: "smtps", 587: "submission", 853: "dot", 993: "imaps", 995: "pop3s",
	3478: "stun", 5228: "gcm", 5353: "mdns", 8080: "http-alt", 51820: "wg", 51821: "wg",
}

// namer resolves IPs to hostnames in the background; lookups never block sampling.
type namer struct {
	mu      sync.Mutex
	names   map[string]string
	pending map[string]bool
}

func newNamer() *namer {
	return &namer{names: map[string]string{}, pending: map[string]bool{}}
}

func (n *namer) lookup(ip string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if name, ok := n.names[ip]; ok {
		return name
	}
	if !n.pending[ip] {
		n.pending[ip] = true
		go n.resolve(ip)
	}
	return ip
}

func (n *namer) resolve(ip string) {
	ctx, cancel := context.WithTimeout(context.Background(), reverseLookupTimeout)
	defer cancel()
	name := ip
	if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
		name = strings.TrimSuffix(names[0], ".")
	}
	n.mu.Lock()
	n.names[ip] = name
	delete(n.pending, ip)
	n.mu.Unlock()
}

func serviceName(port uint32) string {
	if port > 0xffff {
		return strconv.Itoa(int(port))
	}
	if name, ok := wellKnownPorts[uint16(port)]; ok {
		return name
	}
	return strconv.Itoa(int(port))
}
