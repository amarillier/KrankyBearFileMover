#!/bin/bash

# Install or update Fyne for Linux systems
go get fyne.io/fyne/v2@latest
go mod download
go mod tidy
