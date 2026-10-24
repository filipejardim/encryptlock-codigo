//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"syscall"
)

// background runs the program on its own session (it keeps running after
// pc-autostart exits).
func background(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// hidden runs a console program with no window.
func hidden(c *exec.Cmd) { background(c) }

// processAlive reports whether the process with this id is still running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

// processIs: elsewhere only whether the process is running.
func processIs(pid int, _ string) bool { return processAlive(pid) }

// StartupArgs: only the Microsoft Store package starts lockkeeper with no
// arguments (Windows).
func StartupArgs() []string { return nil }
