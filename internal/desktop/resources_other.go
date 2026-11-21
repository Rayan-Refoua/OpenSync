//go:build !windows

package desktop

import "fmt"

func readResourceUsage() (resourceReading, error) {
	return resourceReading{}, fmt.Errorf("process-tree CPU and resident-memory monitoring is currently available on Windows only")
}
