//go:build !windows

package main

// prepareWindowsCLI is a no-op on non-Windows builds.
func prepareWindowsCLI() {}

// installWindowsCtrlHandler is a no-op on non-Windows builds; Ctrl+C is handled by os/signal there.
func installWindowsCtrlHandler(stop func()) func() { return func() {} }
