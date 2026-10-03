package capture

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// Start opens a raw AF_PACKET socket that receives frames from every interface.
// It needs CAP_NET_RAW, which the container grants with --cap-add NET_RAW.
func Start(stats *Stats) error {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ALL)))
	if err != nil {
		return fmt.Errorf("packet socket: %w (needs CAP_NET_RAW)", err)
	}
	stats.SetEnabled()
	go read(fd, stats)
	return nil
}

func read(fd int, stats *Stats) {
	defer unix.Close(fd)
	buf := make([]byte, 65536)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			stats.SetError(err.Error())
			return
		}
		if p, ok := Decode(buf[:n]); ok {
			stats.Add(p)
		}
	}
}

func htons(v uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return binary.LittleEndian.Uint16(b)
}
