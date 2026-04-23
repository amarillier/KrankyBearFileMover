//go:build !windows

package main

// prepareWindowsCLI is a no-op on non-Windows builds.
func prepareWindowsCLI() {}
