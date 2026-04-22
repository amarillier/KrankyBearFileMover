#!/bin/bash

_CONFIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${_CONFIG_DIR}/build-config.sh"

cd "${_CONFIG_DIR}" || exit 1

echo "${KB_PROJECT_TITLE} - macOS Compile Script"
echo "================================================"
echo ""

cp ReleaseNotes.txt assets
[ -f LICENSE ] && cp LICENSE assets/License.txt

# Primary i18n step: merge locale keys + refresh embedded English (requires Python 3.7+).
# Linux/Windows compile scripts skip this; rsync from Mac brings updated embedded_defaults.
if [ -f tools/i18n_sync_for_build.sh ]; then
    ./tools/i18n_sync_for_build.sh || exit 1
fi

# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    mkdir -p bin
fi

# Remove current macOS artifacts and legacy names (older scripts used filemover-macos-*).
rm -f "bin/${KB_FPM_NAME}"-macos-*
rm -f bin/filemover-macos-amd64 bin/filemover-macos-arm64

# Check if Go is installed
if ! command -v go &> /dev/null
then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

# Use committed go.mod only (do not bump Fyne here — same policy as compile-windows.ps1).
export GOWORK=off
go mod download || exit 1

echo "Note: Build only macOS binaries here."
echo "      Outputs: bin/${KB_FPM_NAME}-macos-{amd64,arm64} and ./${KB_LINUX_TEST_BINARY} (native arch, for ./buildrun.sh)."
echo ""

NATIVE_ARCH="$(uname -m)"
[[ "$NATIVE_ARCH" == "x86_64" ]] && NATIVE_ARCH=amd64

echo "Building for macOS (Intel)..."
if ! GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -buildvcs=false -ldflags="-s -w" -trimpath -o "bin/${KB_FPM_NAME}-macos-amd64" .
then
    echo "✗ macOS (Intel) build failed (use macOS to build for macOS)"
    exit 1
fi
echo "✓ macOS (Intel) build successful"

echo ""
echo "Building for macOS (Apple Silicon)..."
ARM64_OK=0
if GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -buildvcs=false -ldflags="-s -w" -trimpath -o "bin/${KB_FPM_NAME}-macos-arm64" .
then
    echo "✓ macOS (Apple Silicon) build successful"
    ARM64_OK=1
else
    echo "✗ macOS (Apple Silicon) build failed"
    if [[ "$NATIVE_ARCH" == "arm64" ]]
    then
        echo "  (native host is Apple Silicon — fix the ARM64 build before packaging)"
        exit 1
    fi
    echo "  (non-fatal on Intel Mac if you do not cross-compile for arm64)"
fi

echo "Setting icons"
./setIcon.sh "$KB_MAC_ICON" "bin/${KB_FPM_NAME}-macos-amd64"
if [[ "$ARM64_OK" -eq 1 ]]
then
    ./setIcon.sh "$KB_MAC_ICON" "bin/${KB_FPM_NAME}-macos-arm64"
fi

# Same convenience as compile-linux.sh: native binary at ./filemover for ./buildrun.sh workflows.
if [[ "$NATIVE_ARCH" == "arm64" ]] && [[ "$ARM64_OK" -eq 1 ]]
then
    cp -f "bin/${KB_FPM_NAME}-macos-arm64" "./${KB_LINUX_TEST_BINARY}"
elif [[ "$NATIVE_ARCH" == "amd64" ]]
then
    cp -f "bin/${KB_FPM_NAME}-macos-amd64" "./${KB_LINUX_TEST_BINARY}"
fi
if [ -f "./${KB_LINUX_TEST_BINARY}" ]
then
    chmod +x "./${KB_LINUX_TEST_BINARY}"
    ./setIcon.sh "$KB_MAC_ICON" "./${KB_LINUX_TEST_BINARY}"
fi

echo ""
echo "================================================"
echo "Compile complete! Binaries are in the bin/ directory."
ls -lh bin/

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
