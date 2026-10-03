package capture

import (
	"encoding/binary"
	"testing"
	"time"
)

func ipv4Frame(proto byte, src, dst [4]byte, l4 []byte) []byte {
	eth := make([]byte, 14)
	binary.BigEndian.PutUint16(eth[12:], 0x0800)
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(20+len(l4)))
	ip[8] = 64
	ip[9] = proto
	copy(ip[12:16], src[:])
	copy(ip[16:20], dst[:])
	return append(append(eth, ip...), l4...)
}

func tcpSegment(sport, dport uint16, payload []byte) []byte {
	t := make([]byte, 20)
	binary.BigEndian.PutUint16(t[0:], sport)
	binary.BigEndian.PutUint16(t[2:], dport)
	t[12] = 5 << 4
	return append(t, payload...)
}

func udpDatagram(sport, dport uint16, payload []byte) []byte {
	u := make([]byte, 8)
	binary.BigEndian.PutUint16(u[0:], sport)
	binary.BigEndian.PutUint16(u[2:], dport)
	binary.BigEndian.PutUint16(u[4:], uint16(8+len(payload)))
	return append(u, payload...)
}

// clientHello builds a minimal TLS 1.2-style ClientHello record carrying an SNI.
func clientHello(sni string) []byte {
	ext := []byte{0x00, 0x00}
	name := append([]byte{0x00, byte(len(sni) >> 8), byte(len(sni))}, []byte(sni)...)
	listLen := len(name)
	sniExt := append([]byte{byte(listLen >> 8), byte(listLen)}, name...)
	extBody := append([]byte{0x00, 0x00, byte(len(sniExt) >> 8), byte(len(sniExt))}, sniExt...)
	_ = ext
	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)                   // session id length
	body = append(body, 0x00, 0x02, 0x13, 0x01) // one cipher suite
	body = append(body, 0x01, 0x00)             // compression
	body = append(body, byte(len(extBody)>>8), byte(len(extBody)))
	body = append(body, extBody...)
	hs := append([]byte{0x01, 0, byte(len(body) >> 8), byte(len(body))}, body...)
	return append([]byte{0x16, 0x03, 0x01, byte(len(hs) >> 8), byte(len(hs))}, hs...)
}

func dnsQuery(name string, qtype uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[4:], 1) // one question
	for _, label := range splitLabels(name) {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = append(msg, byte(qtype>>8), byte(qtype), 0, 1)
	return msg
}

func splitLabels(name string) []string {
	var out []string
	cur := ""
	for _, c := range name {
		if c == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestDecodeTLSSNI(t *testing.T) {
	frame := ipv4Frame(6, [4]byte{192, 168, 2, 10}, [4]byte{93, 184, 216, 34},
		tcpSegment(51000, 443, clientHello("www.example.org")))
	p, ok := Decode(frame)
	if !ok || p.Proto != "tcp" || p.SNI != "www.example.org" || p.Dst != "93.184.216.34:443" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestDecodeDNSQuestion(t *testing.T) {
	frame := ipv4Frame(17, [4]byte{192, 168, 2, 10}, [4]byte{192, 168, 2, 88},
		udpDatagram(40000, 53, dnsQuery("nas.example.lan", 28)))
	p, ok := Decode(frame)
	if !ok || p.Proto != "udp" || p.DNSName != "nas.example.lan" || p.DNSType != "AAAA" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestDecodeIgnoresNonIP(t *testing.T) {
	frame := make([]byte, 60)
	binary.BigEndian.PutUint16(frame[12:], 0x0806) // ARP
	if _, ok := Decode(frame); ok {
		t.Fatal("ARP should not decode as IP")
	}
}

func TestDecodeTruncatedFramesDoNotPanic(t *testing.T) {
	full := ipv4Frame(6, [4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2}, tcpSegment(1, 443, clientHello("a.b")))
	for i := 0; i < len(full); i++ {
		Decode(full[:i])
	}
}

func TestStatsRatesAndTopLists(t *testing.T) {
	s := NewStats()
	s.SetEnabled()
	for i := 0; i < 3; i++ {
		p, _ := Decode(ipv4Frame(6, [4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2}, tcpSegment(1, 443, clientHello("api.example"))))
		s.Add(p)
	}
	snap := s.Snapshot(time.Now())
	if snap.Packets != 3 || snap.SNI[0].Name != "api.example" || snap.SNI[0].Count != 3 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}
