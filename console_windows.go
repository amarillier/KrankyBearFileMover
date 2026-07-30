//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// windowsWantsParentConsole is true when the user likely expects stderr/stdout in a terminal
// (help or file-agent flags). GUI builds use -H windowsgui, which detaches from the console unless we attach.
func windowsWantsParentConsole(args []string) bool {
	for _, a := range args[1:] {
		la := strings.ToLower(strings.TrimSpace(a))
		if la == "-h" || la == "--help" || la == "-?" || la == "/?" || la == "-version" || la == "--version" {
			return true
		}
		if la == "-file-agent-window" {
			continue
		}
		if strings.HasPrefix(a, "-file-agent") {
			return true
		}
	}
	return false
}

// prepareWindowsCLI attaches to the parent process console when appropriate so PowerShell/cmd
// show agent output and flag.Usage() for windowsgui binaries.
func prepareWindowsCLI() {
	if !windowsWantsParentConsole(os.Args) {
		return
	}
	// ATTACH_PARENT_PROCESS = (DWORD)-1 — attach to parent console (e.g. PowerShell).
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	procAttach := kernel32.NewProc("AttachConsole")
	r0, _, _ := procAttach.Call(^uintptr(0))
	if r0 == 0 {
		return
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = out
	os.Stderr = out
}

// Windows console control signal values (wincon.h); golang.org/x/sys/windows has no
// SetConsoleCtrlHandler wrapper, so the proc is invoked directly, as with AttachConsole above.
const (
	winCtrlCEvent        = 0
	winCtrlBreakEvent    = 1
	winCtrlCloseEvent    = 2
	winCtrlLogoffEvent   = 5
	winCtrlShutdownEvent = 6
)

// installWindowsCtrlHandler registers an explicit console control handler that calls stop on
// Ctrl+C/Ctrl+Break/console-close/logoff/shutdown. AttachConsole (above) resets the process's
// control-handler table to its default state, which can silently discard Go's own os/signal
// registration depending on init ordering; registering our own handler after AttachConsole
// guarantees Ctrl+C reaches stop() regardless of that ordering. Returns a function that removes it.
func installWindowsCtrlHandler(stop func()) func() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	procSetHandler := kernel32.NewProc("SetConsoleCtrlHandler")

	handler := syscall.NewCallback(func(ctrlType uint32) uintptr {
		switch ctrlType {
		case winCtrlCEvent, winCtrlBreakEvent, winCtrlCloseEvent, winCtrlLogoffEvent, winCtrlShutdownEvent:
			stop()
			return 1 // TRUE: signal handled, skip the default terminate-process handler
		}
		return 0
	})
	procSetHandler.Call(handler, 1) // Add=TRUE

	return func() {
		procSetHandler.Call(handler, 0) // Add=FALSE
	}
}
