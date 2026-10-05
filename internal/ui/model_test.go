package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"netscope/internal/capture"
	"netscope/internal/collector"
)

func fixture(now time.Time) collector.Snapshot {
	conns := []collector.Conn{
		{Process: "curl", PID: 101, Local: "10.0.0.5:50001", Remote: "one.example.com:https", State: "ESTABLISHED", RxBps: 2e6, TxBps: 1e3, RTTms: 12, HasIO: true},
		{Process: "curl", PID: 101, Local: "10.0.0.5:50002", Remote: "two.example.com:https", State: "ESTABLISHED", RxBps: 1e5, TxBps: 2e3, RTTms: -1, HasIO: true},
		{Process: "chrome", PID: 202, Local: "10.0.0.5:50003", Remote: "203.0.113.7:443", State: "ESTABLISHED", RxBps: 5e4, HasIO: true, RTTms: 30},
		{Process: "chrome", PID: 203, Local: "10.0.0.5:50004", Remote: "203.0.113.8:443", State: "ESTABLISHED", HasIO: false, RTTms: -1},
	}
	return collector.Snapshot{
		At:         now,
		Interfaces: []collector.Interface{{Name: "en0", Up: true, RxBps: 2.1e6, TxBps: 1e3, RxTotal: 5e9, TxTotal: 1e8}, {Name: "lo0", Up: true}},
		TCPStates:  map[string]int{"ESTABLISHED": 4, "LISTEN": 2},
		Conns:      conns,
		Targets:    []collector.Probe{{Kind: "gateway", Target: "192.168.2.1:53", RTT: 4 * time.Millisecond}},
		DNS:        []collector.DNSProbe{{Server: "192.168.2.88", RTT: 9 * time.Millisecond, Rcode: "NOERROR"}},
		RxBps:      2.1e6, TxBps: 1e3,
	}
}

func newFixtureModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := New(nil, time.Second)
	m.width, m.height = w, h
	m.record(fixture(time.Now()))
	m.record(fixture(time.Now()))
	return m
}

func send(t *testing.T, m Model, k string) Model {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "space":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestGroupsBusiestFirst(t *testing.T) {
	gs := groupsOf(fixture(time.Now()).Conns)
	if len(gs) != 2 || gs[0].name != "curl" || gs[1].name != "chrome" {
		t.Fatalf("unexpected order: %+v", gs)
	}
}

func TestSelectionAndFoldKeys(t *testing.T) {
	m := newFixtureModel(t, 130, 46)
	if m.selName != "curl" {
		t.Fatalf("default selection = %q, want curl", m.selName)
	}
	m = send(t, m, "down")
	if m.selName != "chrome" {
		t.Fatalf("after down, selection = %q", m.selName)
	}
	m = send(t, m, "space")
	if !m.expanded["chrome"] {
		t.Fatal("space should expand the selected process")
	}
	m = send(t, m, "z")
	if m.expanded["curl"] != false || m.expanded["chrome"] != false {
		t.Fatal("z should collapse all when something is open")
	}
	m = send(t, m, "z")
	if !m.expanded["curl"] || !m.expanded["chrome"] {
		t.Fatal("z should expand all when everything is closed")
	}
}

func TestTabsLiteAndPauseKeys(t *testing.T) {
	m := newFixtureModel(t, 130, 46)
	m = send(t, m, "2")
	if m.tab != 1 {
		t.Fatalf("2 should open the connections tab, tab=%d", m.tab)
	}
	m = send(t, m, "0")
	if m.tab != 9 {
		t.Fatalf("0 should open egress, tab=%d", m.tab)
	}
	m = send(t, m, "1")
	if m.tab != 0 {
		t.Fatal("1 should return to the dashboard")
	}
	m = send(t, m, "V")
	if !m.lite {
		t.Fatal("V should switch to lite view")
	}
	m = send(t, m, "p")
	if !m.paused || !strings.Contains(m.View(), "paused") {
		t.Fatal("p should pause and show it")
	}
}

func TestFilterKeysNarrowConnections(t *testing.T) {
	m := newFixtureModel(t, 130, 46)
	m = send(t, m, "2")
	m = send(t, m, "/")
	for _, r := range "chrome" {
		m = send(t, m, string(r))
	}
	m = send(t, m, "enter")
	if m.filter != "chrome" || len(m.visibleConns()) != 2 {
		t.Fatalf("filter=%q visible=%d", m.filter, len(m.visibleConns()))
	}
	m = send(t, m, "esc")
	if m.filter != "" {
		t.Fatal("esc should clear the filter")
	}
}

func TestFreezeStopsUpdates(t *testing.T) {
	m := newFixtureModel(t, 130, 46)
	before := len(m.rxHist)
	m = send(t, m, "f")
	m.record(fixture(time.Now()))
	if len(m.rxHist) != before {
		t.Fatal("frozen display should not record new samples")
	}
}

// Every rendered line must be exactly the terminal width, or panel borders drift.
func TestViewLinesFitWidth(t *testing.T) {
	for _, size := range [][2]int{{130, 46}, {120, 44}, {100, 30}, {80, 24}} {
		for _, lite := range []bool{false, true} {
			m := newFixtureModel(t, size[0], size[1])
			m.lite = lite
			for _, line := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(line); w != size[0] {
					t.Fatalf("size %v lite=%v: line width %d != %d: %q", size, lite, w, size[0], line)
				}
			}
		}
	}
}

func TestDetailShowsSelectedProcess(t *testing.T) {
	m := newFixtureModel(t, 130, 46)
	v := m.View()
	if !strings.Contains(v, "connections to 2 hosts") {
		t.Fatal("detail block should describe the selected process")
	}
}

func TestEveryTabFitsWidth(t *testing.T) {
	for tab := 0; tab < len(tabNames); tab++ {
		for _, enabled := range []bool{false, true} {
			m := newFixtureModel(t, 130, 46)
			m.tab = tab
			if enabled {
				m.snap.Capture = capture.Snapshot{
					Enabled: true, PPS: 120, BPS: 9e4, Packets: 5000,
					Protocols: map[string]uint64{"tcp": 80, "udp": 20},
					DNS:       []capture.Name{{Name: "nas.example.lan", Count: 4}},
					SNI:       []capture.Name{{Name: "api.example.org", Count: 9}},
					Flows:     []capture.Flow{{Src: "10.0.0.5:51000", Dst: "93.184.216.34:443", Proto: "tcp", Bytes: 2048}},
					Recent:    []capture.Packet{{Proto: "udp", Src: "10.0.0.5:40000", Dst: "192.168.2.88:53", DNSName: "nas.example.lan", DNSType: "A"}},
				}
			}
			for _, line := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(line); w != 130 {
					t.Fatalf("tab=%d capture=%v: line width %d: %q", tab, enabled, w, line)
				}
			}
		}
	}
}

func TestProcessListSaysWhenItIsCut(t *testing.T) {
	m := newFixtureModel(t, 130, 20)
	var conns []collector.Conn
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("proc%02d", i)
		conns = append(conns, collector.Conn{Process: name, PID: int32(1000 + i), Remote: "10.0.0.1:443", State: "ESTABLISHED"})
	}
	m.snap.Conns = conns
	if !strings.Contains(m.View(), "more processes") {
		t.Fatal("a short panel should say how many processes are hidden")
	}
}
