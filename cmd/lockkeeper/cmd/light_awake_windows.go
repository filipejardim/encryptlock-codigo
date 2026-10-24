//go:build windows

package cmd

import (
	"runtime"
	"syscall"
)

const (
	esSystemRequired = 0x00000001
	esContinuous     = 0x80000000
)

// keepAwake tells Windows that the computer is in use while the LockKeeper
// runs, so it does not fall asleep by itself. Only the sleep timer is held:
// the screen still turns off, and sleeping or switching off by hand works as
// always. Windows keeps the request while the thread that made it lives, so
// that thread is kept for good; it ends with the program.
func keepAwake() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	go func() {
		runtime.LockOSThread()
		_, _, _ = proc.Call(esContinuous | esSystemRequired)
		select {}
	}()
}
