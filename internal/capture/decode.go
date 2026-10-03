package capture

import (
	"encoding/binary"
	"net"
	"strconv"
	"strings"
)

// Packet is the summary of one captured frame.
type Packet struct {
	Src, Dst string // "ip:port" for TCP/UDP, bare IP otherwise
	Proto    string // tcp, udp, icmp, icmpv6, other
	Len      int
	DNSName  string // query name for DNS messages
	DNSType  string
	SNI      string // server name from a TLS ClientHello
}

// Decode parses an Ethernet frame. It returns false when the frame is not IPv4 or IPv6.
func Decode(frame []byte) (Packet, bool) {
	p := Packet{Len: len(frame)}
	if len(frame) < 14 {
		return p, false
	}
	etype := binary.BigEndian.Uint16(frame[12:14])
	off := 14
	if etype == 0x8100 || etype == 0x88a8 { // VLAN tag
		if len(frame) < 18 {
			return p, false
		}
		etype = binary.BigEndian.Uint16(frame[16:18])
		off = 18
	}

	var proto byte
	var src, dst string
	var payload []byte
	switch etype {
	case 0x0800:
		b := frame[off:]
		if len(b) < 20 {
			return p, false
		}
		ihl := int(b[0]&0x0f) * 4
		if ihl < 20 || len(b) < ihl {
			return p, false
		}
		proto = b[9]
		src = net.IP(b[12:16]).String()
		dst = net.IP(b[16:20]).String()
		payload = b[ihl:]
	case 0x86dd:
		b := frame[off:]
		if len(b) < 40 {
			return p, false
		}
		proto = b[6]
		src = net.IP(b[8:24]).String()
		dst = net.IP(b[24:40]).String()
		payload = b[40:]
	default:
		return p, false
	}

	switch proto {
	case 6: // TCP
		if len(payload) < 20 {
			p.Proto = "tcp"
			p.Src, p.Dst = src, dst
			return p, true
		}
		sport := binary.BigEndian.Uint16(payload[0:2])
		dport := binary.BigEndian.Uint16(payload[2:4])
		hlen := int(payload[12]>>4) * 4
		p.Proto = "tcp"
		p.Src = joinHostPort(src, sport)
		p.Dst = joinHostPort(dst, dport)
		if hlen >= 20 && len(payload) > hlen && dport == 443 {
			p.SNI = clientHelloSNI(payload[hlen:])
		}
	case 17: // UDP
		if len(payload) < 8 {
			p.Proto = "udp"
			p.Src, p.Dst = src, dst
			return p, true
		}
		sport := binary.BigEndian.Uint16(payload[0:2])
		dport := binary.BigEndian.Uint16(payload[2:4])
		p.Proto = "udp"
		p.Src = joinHostPort(src, sport)
		p.Dst = joinHostPort(dst, dport)
		if sport == 53 || dport == 53 {
			p.DNSName, p.DNSType = dnsQuestion(payload[8:])
		}
	case 1:
		p.Proto, p.Src, p.Dst = "icmp", src, dst
	case 58:
		p.Proto, p.Src, p.Dst = "icmpv6", src, dst
	default:
		p.Proto, p.Src, p.Dst = "other", src, dst
	}
	return p, true
}

func joinHostPort(ip string, port uint16) string {
	return net.JoinHostPort(ip, strconv.Itoa(int(port)))
}

var dnsTypes = map[uint16]string{1: "A", 28: "AAAA", 5: "CNAME", 15: "MX", 16: "TXT", 33: "SRV", 65: "HTTPS", 12: "PTR", 6: "SOA", 2: "NS"}

// dnsQuestion reads the first question name and type from a DNS message body.
func dnsQuestion(msg []byte) (string, string) {
	if len(msg) < 12 {
		return "", ""
	}
	if binary.BigEndian.Uint16(msg[4:6]) == 0 { // no questions
		return "", ""
	}
	name, off, ok := readName(msg, 12)
	if !ok || off+4 > len(msg) {
		return "", ""
	}
	qtype := binary.BigEndian.Uint16(msg[off : off+2])
	if t, found := dnsTypes[qtype]; found {
		return name, t
	}
	return name, "TYPE" + strconv.Itoa(int(qtype))
}

// readName decodes a DNS name without compression pointers (questions never need them).
func readName(msg []byte, off int) (string, int, bool) {
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, false
		}
		l := int(msg[off])
		if l == 0 {
			return strings.Join(labels, "."), off + 1, true
		}
		if l&0xc0 != 0 || off+1+l > len(msg) {
			return "", 0, false
		}
		labels = append(labels, string(msg[off+1:off+1+l]))
		off += 1 + l
		if len(labels) > 127 {
			return "", 0, false
		}
	}
}

// clientHelloSNI extracts the server_name extension from a TLS ClientHello record.
func clientHelloSNI(b []byte) string {
	// TLS record: type(1) version(2) length(2), then handshake: type(1) length(3).
	if len(b) < 9 || b[0] != 0x16 || b[1] != 0x03 || b[5] != 0x01 {
		return ""
	}
	p := 9 + 2 + 32 // handshake header, client version, random
	if p >= len(b) {
		return ""
	}
	sidLen := int(b[p])
	p += 1 + sidLen
	if p+2 > len(b) {
		return ""
	}
	csLen := int(binary.BigEndian.Uint16(b[p : p+2]))
	p += 2 + csLen
	if p >= len(b) {
		return ""
	}
	compLen := int(b[p])
	p += 1 + compLen
	if p+2 > len(b) {
		return ""
	}
	extTotal := int(binary.BigEndian.Uint16(b[p : p+2]))
	p += 2
	end := p + extTotal
	if end > len(b) {
		end = len(b)
	}
	for p+4 <= end {
		etype := binary.BigEndian.Uint16(b[p : p+2])
		elen := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		p += 4
		if p+elen > end {
			return ""
		}
		if etype == 0 { // server_name
			ext := b[p : p+elen]
			if len(ext) < 5 || ext[2] != 0 {
				return ""
			}
			nlen := int(binary.BigEndian.Uint16(ext[3:5]))
			if 5+nlen > len(ext) {
				return ""
			}
			return string(ext[5 : 5+nlen])
		}
		p += elen
	}
	return ""
}
