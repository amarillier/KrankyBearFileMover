#!/usr/bin/env bash
# Bump the app version everywhere it matters. Canonical packaging default: KB_VERSION_DEFAULT in build-config.sh
# (package.sh sources that file). Usage: ./setver.sh 0.2.1
#
# macOS/BSD sed: sed -i '' …   Linux GNU sed: sed -i …

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

# shellcheck disable=SC1091
source "${ROOT}/build-config.sh"

if [[ "$(uname -s)" == Darwin ]]; then
  sed_i() { sed -i '' "$@"; }
else
  sed_i() { sed -i "$@"; }
fi

if [[ $# -ge 1 ]]; then
  ver=$1
else
  cur_pkg="${KB_VERSION_DEFAULT}"
  cur_go="$(grep -E '^[[:space:]]*appVersion[[:space:]]*=' main.go | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"
  echo "Enter a version number (packaging default: ${cur_pkg}, main.go appVersion: ${cur_go})"
  read -r ver
  if [[ -z "${ver}" ]]; then
    echo "No version change; exiting."
    exit 0
  fi
fi

echo "Setting version: ${ver}"

echo "build-config.sh (KB_VERSION_DEFAULT)"
sed_i "s/^export KB_VERSION_DEFAULT=.*/export KB_VERSION_DEFAULT=\"${ver}\"/" ./build-config.sh

echo "main.go (appVersion)"
sed_i "s/^\([[:space:]]*appVersion[[:space:]]*=[[:space:]]*\"\)[^\"]*\(\".*\)/\1${ver}\2/" main.go

echo "FyneApp.toml"
sed_i "s/^Version = \".*\"/Version = \"${ver}\"/" FyneApp.toml

echo "Inno/${KB_INNO_ISS##*/}"
sed_i "s/#define MyAppVersion \".*\"/#define MyAppVersion \"${ver}\"/" "./${KB_INNO_ISS}"

echo "winres/winres.json"
sed_i "s/\"file_version\": \"[^\"]*\"/\"file_version\": \"${ver}\"/" ./winres/winres.json
sed_i "s/\"product_version\": \"[^\"]*\"/\"product_version\": \"${ver}\"/" ./winres/winres.json
sed_i "s/\"FileVersion\": \"[^\"]*\"/\"FileVersion\": \"${ver}\"/" ./winres/winres.json
sed_i "s/\"ProductVersion\": \"[^\"]*\"/\"ProductVersion\": \"${ver}\"/" ./winres/winres.json

echo "Info-plist.txt (CFBundleShortVersionString only)"
sed_i "/<key>CFBundleShortVersionString<\\/key>/,/<string>/ s/<string>[^<]*<\\/string>/<string>${ver}<\\/string>/" ./Info-plist.txt

echo "LICENSE / ReleaseNotes.txt → assets/"
cp LICENSE assets/ 2>/dev/null || true
cp ReleaseNotes.txt assets/
[[ -f LICENSE ]] && cp LICENSE assets/License.txt

echo "Done. Re-source build-config or open a new shell before packaging."

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
