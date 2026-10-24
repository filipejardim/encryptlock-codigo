//go:build !windows

package cmd

import "syscall"

// lowerPriority makes the system give this program the processor only after
// the programs the owner is using.
func lowerPriority() { _ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10) }
