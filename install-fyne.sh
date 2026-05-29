#!/bin/bash

# Fetch the project's pinned dependencies for Linux systems.
# Honors the committed go.mod/go.sum so the build matches the macOS/Windows
# builds. Do NOT "go get fyne@latest" here — an unpinned upgrade rewrites go.mod
# and diverges from (and can break) the pinned, reproducible build.
go mod download
go mod tidy
