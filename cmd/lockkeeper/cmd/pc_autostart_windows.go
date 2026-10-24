//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow      = 0x08000000
	detachedProcess     = 0x00000008
	processQueryLimited = 0x1000
	stillActive         = 259
)

// background runs the program with no window, on its own (it keeps running
// after pc-autostart exits).
func background(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | detachedProcess}
}

// hidden runs a console program (PowerShell) with no window. Unlike
// background it keeps a hidden console: PowerShell does not start without one.
func hidden(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// processAlive reports whether the process with this id is still running
// and is this same program. After Windows restarts, an old id can belong to
// any other program: that one is not "the LockKeeper running".
func processAlive(pid int) bool {
	self, err := os.Executable()
	return err == nil && processIs(pid, filepath.Base(self))
}

// processIs reports whether the process with this id is running the program
// whose file name starts with image (e.g. "cloudflared": the app's
// cloudflared.exe or a versioned copy).
func processIs(pid int, image string) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(processQueryLimited, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n]))), strings.ToLower(image))
}

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentPackageFullName = kernel32.NewProc("GetCurrentPackageFullName")
	procFreeConsole               = kernel32.NewProc("FreeConsole")
)

// packaged reports whether this program runs from the Microsoft Store
// package (MSIX): Windows knows the package it came from.
func packaged() bool {
	if procGetCurrentPackageFullName.Find() != nil {
		return false
	}
	var n uint32
	r, _, _ := procGetCurrentPackageFullName.Call(uintptr(unsafe.Pointer(&n)), 0)
	// ERROR_INSUFFICIENT_BUFFER: there is a package name (no package: 15700).
	return r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || r == 0
}

// StartupArgs is what lockkeeper does when Windows starts it from the
// Microsoft Store package's startup task, which cannot pass arguments:
// pc-autostart for the app's LockKeeper folder (the same command the
// Startup-folder shortcut runs outside the Store). Otherwise nil.
func StartupArgs() []string {
	if len(os.Args) != 1 || !packaged() {
		return nil
	}
	// No black window when Windows starts: let go of the console Windows
	// opened for this program (what it starts has no window either).
	procFreeConsole.Call()
	return []string{"pc-autostart", "--home", filepath.Join(os.Getenv("LOCALAPPDATA"), "EncryptLock", "lockkeeper")}
}
