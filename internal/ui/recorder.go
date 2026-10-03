package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"netscope/internal/collector"
)

// recorder appends one JSON snapshot per line to a timestamped file in the working directory.
type recorder struct {
	f   *os.File
	enc *json.Encoder
	out string
}

func (m *Model) toggleRecording() {
	if m.rec != nil {
		m.closeRecorder()
		m.recMsg = "recording stopped"
		return
	}
	name := fmt.Sprintf("netscope-%s.jsonl", time.Now().Format("20060102-150405"))
	f, err := os.Create(name) // #nosec G304 -- name is a fixed timestamped pattern, not user input
	if err != nil {
		m.recMsg = "record failed: " + err.Error()
		return
	}
	m.rec = &recorder{f: f, enc: json.NewEncoder(f), out: name}
	m.recMsg = "recording to " + name
}

func (m *Model) closeRecorder() {
	if m.rec != nil {
		_ = m.rec.f.Close()
		m.rec = nil
	}
}

func (m *Model) writeRecording(s collector.Snapshot) {
	if m.rec == nil {
		return
	}
	if err := m.rec.enc.Encode(s); err != nil {
		m.recMsg = "record error: " + err.Error()
		m.closeRecorder()
	}
}

// trackProbeEvents records when a target or resolver goes down or comes back.
func (m *Model) trackProbeEvents(s collector.Snapshot) {
	stamp := s.At.Format("15:04:05")
	check := func(key string, down bool) {
		prev, seen := m.probeDown[key]
		if seen && prev == down {
			return
		}
		if !seen && !down {
			m.probeDown[key] = false
			return
		}
		m.probeDown[key] = down
		state := "up"
		if down {
			state = "down"
		}
		m.events = append(m.events, fmt.Sprintf("%s  %s %s", stamp, key, state))
		if len(m.events) > 200 {
			m.events = m.events[len(m.events)-200:]
		}
	}
	for _, p := range s.Targets {
		check(p.Kind+" "+p.Target, p.Err != "")
	}
	for _, d := range s.DNS {
		check("dns "+d.Server, d.Err != "")
	}
}
