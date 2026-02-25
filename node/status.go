//go:build !windows

package node

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/cyclestudy/sbxb/api/xboard"
)

// collectStatus gathers server resource usage (CPU, memory, swap, disk)
// for reporting to the panel. Best-effort: errors are silently ignored.
func collectStatus() *xboard.StatusReport {
	report := &xboard.StatusReport{}

	// Memory + swap from /proc/meminfo (Linux only).
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		info := parseMemInfo(string(data))
		report.Mem.Total = info["MemTotal"] * 1024
		memAvail := info["MemAvailable"]
		if memAvail == 0 {
			// Older kernels without MemAvailable.
			memAvail = info["MemFree"] + info["Buffers"] + info["Cached"]
		}
		report.Mem.Used = (info["MemTotal"] - memAvail) * 1024
		report.Swap.Total = info["SwapTotal"] * 1024
		report.Swap.Used = (info["SwapTotal"] - info["SwapFree"]) * 1024
	}

	// CPU load from /proc/loadavg (Linux only).
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			load, _ := strconv.ParseFloat(fields[0], 64)
			cpuCount := float64(runtime.NumCPU())
			if cpuCount > 0 {
				report.CPU = load / cpuCount * 100
				if report.CPU > 100 {
					report.CPU = 100
				}
			}
		}
	}

	// Disk usage via statfs.
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		report.Disk.Total = int64(stat.Blocks) * int64(stat.Bsize)
		report.Disk.Used = int64(stat.Blocks-stat.Bfree) * int64(stat.Bsize)
	}

	return report
}

// parseMemInfo parses /proc/meminfo into a map of field name -> value in kB.
func parseMemInfo(data string) map[string]int64 {
	result := make(map[string]int64)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		result[key] = val
	}
	return result
}
