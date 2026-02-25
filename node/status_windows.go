//go:build windows

package node

import "github.com/cyclestudy/sbxb/api/xboard"

// collectStatus returns an empty status report on Windows.
func collectStatus() *xboard.StatusReport {
	return &xboard.StatusReport{}
}
