package collector

import (
	"os/exec"
	"runtime"
	"strings"
)

// defaultGateway returns the IPv4 default gateway, or "" when it cannot be determined.
func defaultGateway() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("route", "-n", "get", "default").Output()
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(out), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), ":"); ok && k == "gateway" {
				return strings.TrimSpace(v)
			}
		}
	case "linux":
		out, err := exec.Command("ip", "route", "show", "default").Output()
		if err != nil {
			return ""
		}
		f := strings.Fields(string(out))
		for i, w := range f {
			if w == "via" && i+1 < len(f) {
				return f[i+1]
			}
		}
	}
	return ""
}
