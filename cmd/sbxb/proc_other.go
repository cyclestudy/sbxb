//go:build !linux

package sbxb

import "os/exec"

func setPdeathsig(_ *exec.Cmd) {}
