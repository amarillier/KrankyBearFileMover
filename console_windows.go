//go:build windows

package main

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// windowsWantsParentConsole is true when the user likely expects stderr/stdout in a terminal
// (help or file-agent flags). GUI builds use -H windowsgui, which detaches from the console unless we attach.
func windowsWantsParentConsole(args []string) bool {
	for _, a := range args[1:] {
		la := strings.ToLower(strings.TrimSpace(a))
		if la == "-h" || la == "--help" || la == "-?" || la == "/?" {
			return true
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
