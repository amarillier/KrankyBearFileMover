#!/usr/bin/env bash

set -euo pipefail

echo "KrankyBear FileMover - Windows Compile (bash)"
echo "================================================"
echo

# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    mkdir -p bin
fi

# Cleanup previous Windows binary only (keep other artifacts)
rm -f bin/filemover-windows.exe || true

# Ensure 64-bit CGO build
export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1

# Prefer 64-bit MinGW if available
if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1
then
  export CC="x86_64-w64-mingw32-gcc"
fi

# Clean stale caches/artifacts (ignore errors) - only if needed, or it slows builds
# go clean -cache -testcache >/dev/null 2>&1 || true
find . -name "*386.syso" -type f -exec rm -f {} + 2>/dev/null || true

echo "Building for Windows (GUI, trimmed path)..."
LDFLAGS="-s -w -H windowsgui"
if ! go build -ldflags="$LDFLAGS" -trimpath -o bin/filemover-windows.exe
then
  echo "✗ Windows build failed" >&2
  exit 1
fi

echo "✓ Windows build successful"
ls -lh bin/tailer-windows.exe || true

# Optional packaging with Inno Setup: pass --package to enable
if [[ "${1:-}" == "--package" ]]
then
  echo
  echo "Packaging Windows installer with Inno Setup..."
  cp -f bin/filemover-windows.exe ./KrankyBearFileMover.exe
  ISCC_DEFAULT="/c/Program Files (x86)/Inno Setup 6/ISCC.exe"
  if [[ -x "$ISCC_DEFAULT" || -f "$ISCC_DEFAULT" ]]
  then
    "${ISCC_DEFAULT}" ./Inno/KrankyBearFileMover.iss || {
      echo "✗ Inno Setup packaging failed" >&2
      exit 1
    }
    echo "✓ Inno Setup packaging complete (see installers/)"
  else
    echo "Inno Setup not found at: $ISCC_DEFAULT" >&2
    echo "Install Inno Setup 6 or run packaging from PowerShell with compile-windows.ps1 -Package" >&2
    exit 1
  fi
fi

echo
echo "================================================"
echo "Done."

# "Now this is not even the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
