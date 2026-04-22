#!/usr/bin/env bash

set -euo pipefail
PATH="/opt/homebrew/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/build-config.sh"

# Packager for ${KB_PROJECT_TITLE}
# - macOS: fpm -t osxpkg → ${KB_APP_BUNDLE_APP} in /Applications (optional: packaging/macos/build-pkg.sh for Installer license UI)
# - Linux: fpm — .deb and .rpm (RPM is skipped when this script runs on macOS; build RPM on Linux)
# - Images embedded in binary (repo keeps only icons used by build / Inno / winres / bundled themes)
# - Outputs to ./installers (configurable)

usage() {
  cat <<EOF
Usage: ./package.sh [linux|mac|all] [ENV_VARS]

Arguments:
  linux       Build Linux packages (.deb and .rpm; .rpm only when not on macOS)
  mac         Build macOS package (.pkg)
  all         Build both Linux and macOS packages

Environment variables (optional):
  VERSION     Package version (default: ${KB_VERSION_DEFAULT})
  ITERATION   Package iteration/release (default: 1)
  ARCH        Target arch: amd64|arm64 (default: native for mac, amd64 for linux)
  OUTDIR      Output directory (default: ./installers)
  MAINTAINER  Maintainer (default: ${KB_MAINTAINER_DEFAULT})
  VENDOR      Vendor (default: ${KB_VENDOR_DEFAULT})
  URL         Project URL (default: ${KB_HOMEPAGE})
  LICENSE     License (default: ${KB_LICENSE_DEFAULT})

Examples:
  # Build macOS package
  ./package.sh mac
  ./package.sh mac VERSION=1.2.3 ARCH=amd64
  
  # Build Linux packages (resources are bundled in the package)
  ./package.sh linux
  
  # Build both
  ./package.sh all VERSION=1.2.3
  
  # Install on Linux:
  apt install ./${KB_FPM_NAME}_*.deb
  # or:
  dpkg -i ./${KB_FPM_NAME}_*.deb
EOF
}

# Parse command-line arguments
if [[ $# -eq 0 ]]
then
  usage
  exit 0
fi

TYPE_ARG="${1:-}"
shift  # Remove first argument, keep rest for env vars

# Validate TYPE argument
case "$TYPE_ARG" in
  linux|mac|all)
    # Valid argument
    ;;
  -h|--help|-?)
    usage
    exit 0
    ;;
  *)
    echo "Error: Invalid argument '$TYPE_ARG'. Must be 'linux', 'mac', or 'all'." >&2
    echo ""
    usage
    exit 1
    ;;
esac

# Process remaining arguments as environment variable assignments
# This allows: ./package.sh linux VERSION=1.2.3 ARCH=amd64
for arg in "$@"
do
  if [[ "$arg" == *"="* ]]
  then
    export "$arg"
  fi
done

# Configurable via env vars
NAME=${NAME:-${KB_FPM_NAME}}
VERSION=${VERSION:-${KB_VERSION_DEFAULT}}
ITERATION=${ITERATION:-1}
OUTDIR=${OUTDIR:-./installers}
MAINTAINER=${MAINTAINER:-${KB_MAINTAINER_DEFAULT}}
VENDOR=${VENDOR:-${KB_VENDOR_DEFAULT}}
URL=${URL:-${KB_HOMEPAGE}}
LICENSE=${LICENSE:-${KB_LICENSE_DEFAULT}}

# Function to build packages for a specific type
build_package() {
  local TYPE="$1"
  
  # Architecture handling
  local ARCH="${ARCH:-}"
  if [[ -z "$ARCH" ]]
  then
    if [[ "$TYPE" == "mac" ]]
    then
      # Default to native macOS arch
      ARCH=$(uname -m)
      [[ "$ARCH" == "x86_64" ]] && ARCH=amd64
    else
      ARCH=amd64
    fi
  fi

  case "$ARCH" in
    amd64|x86_64)
      DEB_ARCH=amd64
      RPM_ARCH=x86_64
      PKG_ARCH=amd64
      ;;
    arm64|aarch64)
      DEB_ARCH=arm64
      RPM_ARCH=aarch64
      PKG_ARCH=arm64
      ;;
    *)
      # Fall back to using the same string
      DEB_ARCH="$ARCH"
      RPM_ARCH="$ARCH"
      PKG_ARCH="$ARCH"
      ;;
  esac

  # Source assets - depends on TYPE
  # Determine binary name early for macOS staging
  BIN_NAME="${KB_APP_BUNDLE_EXE}"
  
  if [[ "$TYPE" == "mac" ]]
  then
    SRC_BIN="bin/${KB_FPM_NAME}-macos-${PKG_ARCH}"
    SRC_RESOURCES="assets"
    SRC_INFO_PLIST="Info-plist.txt"
    SRC_README_PLIST="Readme-plist.txt"
    # These will be set from staging directory later
    SRC_IMAGES=""
  else
    SRC_BIN="bin/${KB_FPM_NAME}-linux-amd64"
    SRC_IMAGES="assets/images"
    SRC_RELEASE_NOTES="ReleaseNotes.txt"
    SRC_LICENSE="LICENSE"
  fi

  # Validate sources
  if [[ ! -f "$SRC_BIN" ]]
  then
    if [[ "$TYPE" == "mac" ]]
    then
      echo "Error: Missing $SRC_BIN (build your macOS binary first with ./compile-mac.sh)." >&2
    else
      echo "Error: Missing $SRC_BIN (build your Linux binary first)." >&2
    fi
    exit 1
  fi
  if [[ "$TYPE" == "mac" ]]
  then
    if [[ ! -d "$SRC_RESOURCES" ]]
    then
      echo "Error: Missing directory $SRC_RESOURCES" >&2
      exit 1
    fi
    if [[ ! -d "$SRC_RESOURCES/images" ]]
    then
      echo "Error: Missing directory $SRC_RESOURCES/images" >&2
      exit 1
    fi
    if [[ ! -f "$SRC_INFO_PLIST" ]]
    then
      echo "Error: Missing file $SRC_INFO_PLIST" >&2
      exit 1
    fi
    if [[ ! -f "$SRC_README_PLIST" ]]
    then
      echo "Error: Missing file $SRC_README_PLIST" >&2
      exit 1
    fi
  else
    if [[ ! -d "$SRC_IMAGES" ]]
    then
      echo "Error: Missing directory $SRC_IMAGES" >&2
      exit 1
    fi
    if [[ ! -f "$SRC_RELEASE_NOTES" ]]
    then
      echo "Error: Missing file $SRC_RELEASE_NOTES" >&2
      exit 1
    fi
    if [[ ! -f "$SRC_LICENSE" ]]
    then
      echo "Error: Missing file $SRC_LICENSE" >&2
      exit 1
    fi
  fi

  mkdir -p "$OUTDIR"

  # For macOS: Build complete .app bundle, then use it as source for fpm
  # For Linux packages built on macOS, create staging directory with files
  # stripped of extended attributes to avoid tar header compatibility issues
  STAGING_DIR=""
  APP_BUNDLE=""
  
  if [[ "$TYPE" == "mac" ]]
  then
    # Build the complete .app bundle structure
    APP_BUNDLE="${KB_APP_BUNDLE_APP}"
    echo "Building .app bundle: $APP_BUNDLE..."
    
    # Remove existing bundle if present
    rm -rf "$APP_BUNDLE"
    
    # Create app bundle structure
    mkdir -p "$APP_BUNDLE/Contents/MacOS/assets"
    
    # Copy binary to Contents/MacOS
    cp "$SRC_BIN" "$APP_BUNDLE/Contents/MacOS/$BIN_NAME"
    
    # Symlink CLI-style name -> main executable in Contents/MacOS
    ln -sf "$BIN_NAME" "$APP_BUNDLE/Contents/MacOS/${KB_CLI_NAME}"
    
    # Copy ReleaseNotes.txt and License.txt to assets (UI images are embedded in binary)
    cp "$SRC_RESOURCES/ReleaseNotes.txt" "$APP_BUNDLE/Contents/MacOS/assets/ReleaseNotes.txt"
    if [[ -f "$SRC_RESOURCES/License.txt" ]]; then
      cp "$SRC_RESOURCES/License.txt" "$APP_BUNDLE/Contents/MacOS/assets/License.txt"
    elif [[ -f "LICENSE" ]]; then
      cp "LICENSE" "$APP_BUNDLE/Contents/MacOS/assets/License.txt"
    fi

    # UI locale packs (*.json, *.TESTING.md, help_*.txt) beside the executable: Contents/MacOS/assets/i18n/
    if [[ -d "$SRC_RESOURCES/i18n" ]] && [[ -n "$(ls -A "$SRC_RESOURCES/i18n" 2>/dev/null)" ]]; then
      mkdir -p "$APP_BUNDLE/Contents/MacOS/assets/i18n"
      cp -R "$SRC_RESOURCES/i18n/." "$APP_BUNDLE/Contents/MacOS/assets/i18n/"
    fi
    
    # Set proper permissions on assets directory (755 = rwxr-xr-x)
    chmod -R 755 "$APP_BUNDLE/Contents/MacOS/assets"
    
    # Copy Info-plist.txt and Readme-plist.txt to Contents
    # Note: Using .txt extension to prevent fpm bundle detection issues
    cp "$SRC_INFO_PLIST" "$APP_BUNDLE/Contents/Info-plist.txt"
    cp "$SRC_README_PLIST" "$APP_BUNDLE/Contents/Readme-plist.txt"
    
    # Verify essential files were copied correctly
    if [[ ! -f "$APP_BUNDLE/Contents/MacOS/$BIN_NAME" ]]
    then
      echo "Error: Binary is missing in app bundle" >&2
      exit 1
    fi
    if [[ ! -f "$APP_BUNDLE/Contents/Info-plist.txt" ]]
    then
      echo "Error: Info-plist.txt is missing in app bundle" >&2
      exit 1
    fi
    if [[ ! -f "$APP_BUNDLE/Contents/Readme-plist.txt" ]]
    then
      echo "Error: Readme-plist.txt is missing in app bundle" >&2
      exit 1
    fi
    
    echo "App bundle created successfully."
    
  elif [[ "$TYPE" == "linux" ]]
  then
    echo "Creating staging directory for Linux payload..."
    STAGING_DIR=$(mktemp -d -t fpm-staging.XXXXXX)
    cleanup_staging() { rm -rf "$STAGING_DIR"; }
    trap cleanup_staging EXIT INT TERM
    
    # Copy binary
    if [[ "$(uname -s)" == "Darwin" ]]
    then
      cp -X "$SRC_BIN" "$STAGING_DIR/${KB_FPM_NAME}"
    else
      cp "$SRC_BIN" "$STAGING_DIR/${KB_FPM_NAME}"
    fi
    chmod 755 "$STAGING_DIR/${KB_FPM_NAME}"
    
    # Bundle all assets in this package
    echo "  Mode: Bundled resources"
    
    # Create assets directory structure
    mkdir -p "$STAGING_DIR/assets"
    
    # Copy images (optional payload; main UI art is embedded — keeps installer lean)
    if [[ "$(uname -s)" == "Darwin" ]]
    then
      cp -XR "$SRC_IMAGES" "$STAGING_DIR/assets/images"
    else
      cp -R "$SRC_IMAGES" "$STAGING_DIR/assets/images"
    fi
    SRC_IMAGES_STAGED="$STAGING_DIR/assets/images"
    
    # Copy ReleaseNotes.txt
    cp "$SRC_RELEASE_NOTES" "$STAGING_DIR/assets/ReleaseNotes.txt"
    chmod 644 "$STAGING_DIR/assets/ReleaseNotes.txt"
    SRC_RELEASE_NOTES_STAGED="$STAGING_DIR/assets/ReleaseNotes.txt"
    
    # Copy LICENSE file
    cp "$SRC_LICENSE" "$STAGING_DIR/assets/License.txt"
    chmod 644 "$STAGING_DIR/assets/License.txt"
    SRC_LICENSE_STAGED="$STAGING_DIR/assets/License.txt"

    # UI locale packs for runtime merge (same layout as dev: assets/i18n next to binary)
    if [[ -d "assets/i18n" ]] && [[ -n "$(ls -A assets/i18n 2>/dev/null)" ]]; then
      mkdir -p "$STAGING_DIR/assets/i18n"
      if [[ "$(uname -s)" == "Darwin" ]]; then
        cp -XR assets/i18n/. "$STAGING_DIR/assets/i18n/"
      else
        cp -R assets/i18n/. "$STAGING_DIR/assets/i18n/"
      fi
      chmod -R a+rX "$STAGING_DIR/assets/i18n" 2>/dev/null || true
    fi

    # Aggressively strip any remaining extended attributes from staging directory
    if command -v xattr >/dev/null 2>&1
    then
      xattr -cr "$STAGING_DIR" 2>/dev/null || true
      find "$STAGING_DIR" -name '._*' -delete 2>/dev/null || true
    fi
    if [[ "$(uname -s)" == "Darwin" ]]
    then
      export COPYFILE_DISABLE=1
    fi
    
    # Update source paths to point to staging directory
    SRC_BIN="$STAGING_DIR/${KB_FPM_NAME}"
  fi

#     --license "$LICENSE"
  COMMON_ARGS=(
    -s dir
    -n "$NAME"
    -v "$VERSION"
    --iteration "$ITERATION"
    --maintainer "$MAINTAINER"
    --vendor "$VENDOR"
    --url "$URL"
    --description "$KB_PACKAGE_DESCRIPTION"
    -f
  )

  if [[ "$TYPE" == "mac" ]]
  then
    # macOS .pkg via fpm osxpkg (native arch; avoids productbuild path that prompted Rosetta on some ARM Macs)
    PKG_OUTFILE="$OUTDIR/${KB_FPM_NAME}_${VERSION}-${ITERATION}_${PKG_ARCH}.pkg"
    echo "Building macOS .pkg ($PKG_ARCH) -> $PKG_OUTFILE..."

    APP_NAME="${KB_APP_BUNDLE_APP}"
    APP_DIR="/Applications/$APP_NAME"
    CONTENTS_DIR="$APP_DIR/Contents"
    MACOS_DIR="$CONTENTS_DIR/MacOS"
    RESOURCES_DIR="$MACOS_DIR/assets"

    FPM_FILES=(
      "$APP_BUNDLE/Contents/MacOS/$BIN_NAME=$MACOS_DIR/$BIN_NAME"
      "$APP_BUNDLE/Contents/MacOS/${KB_CLI_NAME}=$MACOS_DIR/${KB_CLI_NAME}"
      "$APP_BUNDLE/Contents/Info-plist.txt=$CONTENTS_DIR/Info-plist.txt"
      "$APP_BUNDLE/Contents/Readme-plist.txt=$CONTENTS_DIR/Readme-plist.txt"
      "$APP_BUNDLE/Contents/MacOS/assets/ReleaseNotes.txt=$RESOURCES_DIR/ReleaseNotes.txt"
    )
    if [[ -f "$APP_BUNDLE/Contents/MacOS/assets/License.txt" ]]; then
      FPM_FILES+=("$APP_BUNDLE/Contents/MacOS/assets/License.txt=$RESOURCES_DIR/License.txt")
    fi

    if [[ -d "$APP_BUNDLE/Contents/MacOS/assets/i18n" ]]; then
      _i18nroot="$APP_BUNDLE/Contents/MacOS/assets/i18n"
      while IFS= read -r _i18nfile; do
        [[ -f "$_i18nfile" ]] || continue
        _sub="${_i18nfile#${_i18nroot}/}"
        FPM_FILES+=("$_i18nfile=$RESOURCES_DIR/i18n/$_sub")
      done < <(find "$_i18nroot" -type f | LC_ALL=C sort)
    fi

    if ! validate_fpm_files "${FPM_FILES[@]}"
    then
      echo "Aborting .pkg build due to validation errors." >&2
      exit 1
    fi

    echo "Building package with fpm..."

    MAC_PREINSTALL=$(mktemp)
    cat > "$MAC_PREINSTALL" << EOF
#!/bin/sh
set -e
if [ -d "/Applications/${KB_APP_BUNDLE_APP}" ]; then
  rm -rf "/Applications/${KB_APP_BUNDLE_APP}"
fi
for _pkg in ${KB_PKGUTIL_FORGET}; do
  pkgutil --forget "\$_pkg" 2>/dev/null || true
done
exit 0
EOF
    chmod +x "$MAC_PREINSTALL"

    if fpm \
      "${COMMON_ARGS[@]}" \
      -t osxpkg \
      -a "$PKG_ARCH" \
      --before-install "$MAC_PREINSTALL" \
      --directories "$APP_DIR" \
      --directories "$CONTENTS_DIR" \
      --directories "$MACOS_DIR" \
      --directories "$RESOURCES_DIR" \
      --directories "$RESOURCES_DIR/i18n" \
      --package "$PKG_OUTFILE" \
      --license "./LICENSE" \
      "${FPM_FILES[@]}"
    then
      rm -f "$MAC_PREINSTALL"
      echo "✓ fpm packaging succeeded"
    else
      rm -f "$MAC_PREINSTALL"
      echo "✗ fpm packaging failed" >&2
      exit 1
    fi

    ./setIcon.sh "$KB_PKG_ICON" "$PKG_OUTFILE"
    echo ""
    echo "Done. Package created:"
    echo "  $PKG_OUTFILE"
    echo ""
    echo "To install:"
    echo "  sudo installer -pkg $PKG_OUTFILE -target /"
    echo "  or double-click the .pkg file"
    echo ""
    echo "To verify installation:"
    echo "  ls -la /Applications/${KB_APP_BUNDLE_APP}"
    echo "  pkgutil --pkg-info ${KB_FPM_NAME}"
    
    # Clean up temporary .app bundle build artifact
    echo "Cleaning up temporary .app bundle..."
    rm -rf "$APP_BUNDLE"
    
  else
    # Linux .deb and .rpm installers
    DEB_OUTFILE="$OUTDIR/${KB_FPM_NAME}_${VERSION}-${ITERATION}_${DEB_ARCH}.deb"
    RPM_OUTFILE="$OUTDIR/${KB_FPM_NAME}_${VERSION}-${ITERATION}_${RPM_ARCH}.rpm"
    
    echo "Building .deb ($DEB_ARCH) -> $DEB_OUTFILE..."
    
    # Create post-uninstall cleanup script for .deb
    # Only runs on purge to clean up any remaining directories
    DEB_POSTRM_SCRIPT=$(mktemp)
    cat > "$DEB_POSTRM_SCRIPT" << EOF
#!/bin/sh
case "\$1" in
    purge)
        rm -rf /opt/${KB_OPT_DIR}
        ;;
esac
exit 0
EOF
    chmod +x "$DEB_POSTRM_SCRIPT"
    
    DEB_PREINST_SCRIPT=$(mktemp)
    cat > "$DEB_PREINST_SCRIPT" << EOF
#!/bin/sh
set -e
case "\$1" in
  install|upgrade)
    rm -rf /opt/${KB_OPT_DIR}
    ;;
esac
exit 0
EOF
    chmod +x "$DEB_PREINST_SCRIPT"
    
    # Build fpm args array for .deb
    # Note: fpm automatically calculates Installed-Size from the staged files
    # No --directories flag to avoid dpkg marking them as conffiles
    DEB_ARGS=(
      "${COMMON_ARGS[@]}"
      -t deb
      -a "$DEB_ARCH"
      --deb-no-default-config-files
      --before-install "$DEB_PREINST_SCRIPT"
      --after-remove "$DEB_POSTRM_SCRIPT"
    )
    
    # Build fpm file list
    # Install binary and docs flat in /opt/${KB_OPT_DIR} (no subdirectories)
    DEB_FPM_FILES=(
      "$SRC_BIN=/opt/${KB_OPT_DIR}/${KB_FPM_NAME}"
    )
    
    # Include ReleaseNotes and LICENSE directly in install directory (not in assets/)
    if [[ -n "$SRC_RELEASE_NOTES_STAGED" ]]
    then
      DEB_FPM_FILES+=("$SRC_RELEASE_NOTES_STAGED=/opt/${KB_OPT_DIR}/ReleaseNotes.txt")
    fi
    if [[ -n "$SRC_LICENSE_STAGED" ]]
    then
      DEB_FPM_FILES+=("$SRC_LICENSE_STAGED=/opt/${KB_OPT_DIR}/License.txt")
    fi

    if [[ -d "$STAGING_DIR/assets/i18n" ]]; then
      while IFS= read -r _i18nfile; do
        [[ -f "$_i18nfile" ]] || continue
        _rel="${_i18nfile#$STAGING_DIR/}"
        DEB_FPM_FILES+=("$_i18nfile=/opt/${KB_OPT_DIR}/$_rel")
      done < <(find "$STAGING_DIR/assets/i18n" -type f | LC_ALL=C sort)
    fi
    
    # Validate file mappings before running fpm
    if ! validate_fpm_files "${DEB_FPM_FILES[@]}"
    then
      echo "Aborting .deb build due to validation errors." >&2
      exit 1
    fi
    
    fpm \
      "${DEB_ARGS[@]}" \
      --package "$DEB_OUTFILE" \
      "${DEB_FPM_FILES[@]}"
    
    # Clean up temporary scripts
    rm -f "$DEB_POSTRM_SCRIPT" "$DEB_PREINST_SCRIPT"

    # rpmbuild via fpm is unreliable on macOS (often exits non-zero); Linux CI / Ubuntu builds RPMs.
    if [[ "$(uname -s)" == "Darwin" ]]
    then
      echo ""
      echo "Skipping .rpm on macOS (fpm/rpmbuild for Linux RPM targets is not supported reliably here)."
      echo "  Built: $DEB_OUTFILE"
      echo "  For .rpm files, run ./package.sh linux on Linux (e.g. ./compile-linux.sh + ./package.sh linux via sync2ubuntu18.sh)."
    else
      echo ""
      echo "Building .rpm ($RPM_ARCH) -> $RPM_OUTFILE..."

      # Create post-uninstall cleanup script for .rpm
      RPM_POSTUN_SCRIPT=$(mktemp)
      cat > "$RPM_POSTUN_SCRIPT" << EOF
#!/bin/sh
# \$1 = 0 means this is a complete removal (not an upgrade)
if [ "\$1" -eq 0 ]; then
    rm -rf /opt/${KB_OPT_DIR}
fi
exit 0
EOF
      chmod +x "$RPM_POSTUN_SCRIPT"

      RPM_PREINST_SCRIPT=$(mktemp)
      cat > "$RPM_PREINST_SCRIPT" << EOF
#!/bin/sh
set -e
rm -rf /opt/${KB_OPT_DIR}
exit 0
EOF
      chmod +x "$RPM_PREINST_SCRIPT"

      # Build RPM package with all files including symlink
      # RPM will auto-create directories from file paths
      RPM_ARGS=(
        "${COMMON_ARGS[@]}"
        -t rpm
        -a "$RPM_ARCH"
        --rpm-os linux
        --rpm-auto-add-directories
        --before-install "$RPM_PREINST_SCRIPT"
        --after-remove "$RPM_POSTUN_SCRIPT"
      )

      # Build fpm file list
      # Install binary and docs flat in /opt/${KB_OPT_DIR} (no subdirectories)
      RPM_FPM_FILES=(
        "$SRC_BIN=/opt/${KB_OPT_DIR}/${KB_FPM_NAME}"
      )

      # Include ReleaseNotes and LICENSE directly in install directory (not in assets/)
      if [[ -n "$SRC_RELEASE_NOTES_STAGED" ]]
      then
        RPM_FPM_FILES+=("$SRC_RELEASE_NOTES_STAGED=/opt/${KB_OPT_DIR}/ReleaseNotes.txt")
      fi
      if [[ -n "$SRC_LICENSE_STAGED" ]]
      then
        RPM_FPM_FILES+=("$SRC_LICENSE_STAGED=/opt/${KB_OPT_DIR}/License.txt")
      fi

      if [[ -d "$STAGING_DIR/assets/i18n" ]]; then
        while IFS= read -r _i18nfile; do
          [[ -f "$_i18nfile" ]] || continue
          _rel="${_i18nfile#$STAGING_DIR/}"
          RPM_FPM_FILES+=("$_i18nfile=/opt/${KB_OPT_DIR}/$_rel")
        done < <(find "$STAGING_DIR/assets/i18n" -type f | LC_ALL=C sort)
      fi

      # Validate file mappings before running fpm
      if ! validate_fpm_files "${RPM_FPM_FILES[@]}"
      then
        echo "Aborting .rpm build due to validation errors." >&2
        exit 1
      fi

      fpm \
        "${RPM_ARGS[@]}" \
        --package "$RPM_OUTFILE" \
        "${RPM_FPM_FILES[@]}"

      # Clean up temporary script
      rm -f "$RPM_POSTUN_SCRIPT" "$RPM_PREINST_SCRIPT"
    fi

    echo ""
    echo "Done. Packages created:"
    echo "  $DEB_OUTFILE"
    if [[ "$(uname -s)" != "Darwin" ]]
    then
      echo "  $RPM_OUTFILE"
    fi
    echo ""
    echo "Install with:"
    echo "  apt install ./$DEB_OUTFILE"
    echo "  # or"
    echo "  dpkg -i ./$DEB_OUTFILE"
    echo ""
    echo "Application installed to: /opt/${KB_OPT_DIR}/ (locale packs under assets/i18n/ when packaged)"
  fi
}

# fpm required for Linux packages and macOS .pkg (osxpkg)
if [[ "$TYPE_ARG" == "linux" || "$TYPE_ARG" == "all" || "$TYPE_ARG" == "mac" ]]
then
  if ! command -v fpm >/dev/null 2>&1
  then
    echo "Error: fpm not found. Install with: gem install fpm" >&2
    exit 1
  fi
fi

# Pre-flight validation for fpm file mappings (Bash 3.x compatible)
# Detects: circular symlinks, duplicate destinations, symlinks overwriting files
validate_fpm_files() {
  local src dest target mapping
  local seen_dests=""
  local seen_srcs=""
  
  for mapping in "$@"
  do
    src="${mapping%%=*}"
    dest="${mapping#*=}"
    
    # Check for circular/broken symlinks
    if [[ -L "$src" ]]
    then
      target=$(readlink "$src" 2>/dev/null || echo "")
      if [[ -z "$target" ]] || [[ "$target" == "$(basename "$src")" ]]
      then
        echo "Error: Circular or broken symlink detected: $src" >&2
        echo "  This will cause fpm to fail with 'destination is a directory' error." >&2
        return 1
      fi
      # Check if symlink target exists
      if [[ ! -e "$src" ]]
      then
        echo "Error: Symlink target does not exist: $src -> $target" >&2
        return 1
      fi
    fi
    
    # Check for duplicate destinations (Bash 3.x compatible using string matching)
    # Use newline-delimited string for tracking seen destinations
    if echo "$seen_dests" | grep -qxF "$dest"
    then
      # Find the original source for this destination
      local orig_src=""
      local check_mapping check_dest
      for check_mapping in "$@"
      do
        check_dest="${check_mapping#*=}"
        if [[ "$check_dest" == "$dest" ]]
        then
          orig_src="${check_mapping%%=*}"
          break
        fi
      done
      echo "Error: Duplicate destination detected: $dest" >&2
      echo "  First source: $orig_src" >&2
      echo "  Second source: $src" >&2
      echo "  This will cause fpm to fail with 'destination is a directory' error." >&2
      return 1
    fi
    seen_dests="$seen_dests
$dest"
    seen_srcs="$seen_srcs
$src"
    
    # Check source exists (for non-symlinks)
    if [[ ! -e "$src" ]] && [[ ! -L "$src" ]]
    then
      echo "Error: Source file does not exist: $src" >&2
      return 1
    fi
  done
  
  return 0
}

# Build packages based on TYPE_ARG
case "$TYPE_ARG" in
  linux)
    build_package "linux"
    ;;
  mac)
    build_package "mac"
    ;;
  all)
    echo "Building all packages..."
    echo ""
    build_package "linux"
    echo ""
    build_package "mac"
    ;;
esac

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
