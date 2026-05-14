// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// newDaemonCmd creates an exec.Cmd for the daemon process detached from the terminal.
func newDaemonCmd(exe string, args ...string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	return cmd
}
