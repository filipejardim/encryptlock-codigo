//go:build windows

package cmd

import (
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// systemAwake reads whether some program holds the computer awake
// (CallNtPowerInformation, SystemExecutionState: no administrator needed).
func systemAwake(t *testing.T) bool {
	var state uint32
	r, _, _ := syscall.NewLazyDLL("powrprof.dll").NewProc("CallNtPowerInformation").Call(16, 0, 0, uintptr(unsafe.Pointer(&state)), 4)
	if r != 0 {
		t.Fatalf("CallNtPowerInformation: 0x%x", r)
	}
	return state&esSystemRequired != 0
}

func TestKeepAwake(t *testing.T) {
	if systemAwake(t) {
		t.Skip("another program already keeps this computer awake")
	}
	keepAwake()
	for i := 0; i < 50 && !systemAwake(t); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !systemAwake(t) {
		t.Fatal("the computer can still fall asleep while the LockKeeper runs")
	}
}
