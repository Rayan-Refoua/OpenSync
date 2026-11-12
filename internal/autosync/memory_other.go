//go:build !windows

package autosync

import (
	"os"
	"strconv"
	"strings"
)

func availableMemory() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "MemAvailable:" {
				n, e := strconv.ParseUint(fields[1], 10, 64)
				if e == nil {
					return n * 1024
				}
			}
		}
	}
	return 2 << 30
}
