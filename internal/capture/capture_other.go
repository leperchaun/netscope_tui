//go:build !linux

package capture

import "errors"

// Start is unsupported off Linux until a BPF or pcap backend exists.
func Start(stats *Stats) error {
	return errors.New("packet capture is only implemented on Linux")
}
