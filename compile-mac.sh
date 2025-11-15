#!/bin/bash

echo "KrankyBear FileMover - macOS Compile Script"
echo "================================================"
echo ""

cp ReleaseNotes.txt Resources
# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    mkdir -p bin
fi

# cleanup any existing binaries
rm -f bin/filemover-mac*

# Check if Go is installed
if ! command -v go &> /dev/null
then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

# fast update fyne before compile
go get fyne.io/fyne/v2@latest # or a specific version like @v2.4.0
go mod tidy
go mod vendor

echo "Note: Build only macOS binaries here."
echo ""

echo ""
# macOS builds (native)
echo "Building for macOS (Intel)..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o bin/filemover-macos-amd64 2>&1
if [ $? -eq 0 ]
then
    echo "✓ macOS (Intel) build successful"
else
    echo "✗ macOS (Intel) build failed (use macOS to build for macOS)"
fi

echo ""
echo "Building for macOS (Apple Silicon)..."
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o bin/filemover-macos-arm64 2>&1
if [ $? -eq 0 ]
then
    echo "✓ macOS (Apple Silicon) build successful"
else
    echo "✗ macOS (Apple Silicon) build failed (use macOS to build for macOS)"
fi

echo "Setting icons"
./setIcon.sh Resources/Images/KrankyBearCowboyBrown.png bin/filemover-macos-amd64
./setIcon.sh Resources/Images/KrankyBearCowboyBrown.png bin/filemover-macos-arm64

echo ""
echo "================================================"
echo "Compile complete! Binaries are in the bin/ directory."
ls -lh bin/

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
