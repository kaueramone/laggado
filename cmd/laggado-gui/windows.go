//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	dwmapi               = syscall.NewLazyDLL("dwmapi.dll")
	procGetForegroundWnd = user32.NewProc("GetForegroundWindow")
	procDwmSetAttribute  = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaBorderColor          = 34
	dwmwaCapColor             = 35
)

// applyWindowsStyle enables dark title bar on Windows 11.
func applyWindowsStyle(w fyne.Window) {
	hwnd, _, _ := procGetForegroundWnd.Call()
	if hwnd == 0 {
		return
	}
	// DWMWA_USE_IMMERSIVE_DARK_MODE = 20, value = 1
	darkMode := uint32(1)
	procDwmSetAttribute.Call(
		hwnd,
		dwmwaUseImmersiveDarkMode,
		uintptr(unsafe.Pointer(&darkMode)),
		4,
	)
}
