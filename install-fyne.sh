#!/bin/bash

# Install or update Fyne for Linux systems
go get fyne.io/fyne/v2@latest
go mod vendor
go mod tidy
