#!/usr/bin/env bash

_CONFIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${_CONFIG_DIR}/build-config.sh"

set -euo pipefail

echo "${KB_PROJECT_TITLE} - Windows Sync, Compile. Package and Sync Back (bash)"
echo "================================================"
echo

# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    mkdir -p bin
fi

# Cleanup previous Windows binary only (keep other artifacts)
rm -f "bin/${KB_WINDOWS_EXE}"
rm -f installers/*

# i18n sync: run on Mac before sync (compile-mac.sh or tools/i18n_sync_for_build.sh).

echo "Windows"
echo "syncing to windows"
./sync2windows.sh

echo "compiling on windows"
./compile-windows-ssh.sh -package

echo "syncing back"
./sync2windows.sh sync-back

echo "Icon is embedded via winres during Windows build"
# Icon is automatically embedded in bin/${KB_WINDOWS_EXE} via rsrc_windows_amd64.syso

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
