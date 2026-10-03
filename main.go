package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"netscope/internal/collector"
	"netscope/internal/ui"
)

func main() {
	interval := flag.Duration("interval", 1*time.Second, "refresh interval")
	dnsFlag := flag.String("dns", "auto", "DNS servers to query: auto (system resolvers), none, or a comma-separated list")
	dnsName := flag.String("dns-name", "example.com", "name to resolve when probing DNS servers")
	targets := flag.String("targets", "", "comma-separated host:port TCP connect targets")
	once := flag.Bool("once", false, "print one JSON snapshot and exit")
	flag.Parse()

	sampler := collector.NewSampler(splitList(*targets), resolvers(*dnsFlag), *dnsName)

	if *once {
		if err := printOnce(sampler); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(ui.New(sampler, *interval), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func resolvers(flagVal string) []string {
	switch flagVal {
	case "none":
		return nil
	case "auto":
		return collector.SystemResolvers()
	default:
		return splitList(flagVal)
	}
}

func printOnce(sampler *collector.Sampler) error {
	// Rates need two samples; take a short baseline first.
	if _, err := sampler.Sample(); err != nil {
		return err
	}
	time.Sleep(time.Second)
	snap, err := sampler.Sample()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

func splitList(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
