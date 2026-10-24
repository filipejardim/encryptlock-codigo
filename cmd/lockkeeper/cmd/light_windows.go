//go:build windows

package cmd

import "syscall"

const belowNormalPriorityClass = 0x00004000

// lowerPriority makes Windows give this program the processor only after
// the programs the owner is using.
func lowerPriority() {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return
	}
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("SetPriorityClass").Call(uintptr(h), belowNormalPriorityClass)
}
