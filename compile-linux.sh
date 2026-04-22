#!/bin/bash

_CONFIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${_CONFIG_DIR}/build-config.sh"

set -e  # Exit on error

echo "${KB_PROJECT_TITLE} - Linux Compile Script"
echo "========================================="
echo ""

# Create bin directory if it doesn't exist
if [ ! -d "bin" ]
then
    echo "creating bin directory"
    mkdir -p bin
fi

# Create installers directory if it doesn't exist
if [ ! -d "installers" ]
then
    echo "creating installers directory"
    mkdir -p installers
fi

# cleanup any existing binaries
rm -f "bin/${KB_FPM_NAME}"*
# delete any existing installers
rm -f "installers/${KB_FPM_NAME}"*

# Check if Go is installed
if ! command -v go &> /dev/null
then
    echo "Error: Go is not installed. Please install Go 1.21 or later."
    # exit 1
    ./install-go.sh
    ./install-fyne.sh
fi

# Function to detect Linux distribution
detect_distro() {
    if [ -f /etc/os-release ]
	then
        . /etc/os-release
        echo "$ID"
    elif [ -f /etc/debian_version ]
	then
        echo "debian"
    elif [ -f /etc/redhat-release ]
    then
        echo "rhel"
    else
        echo "unknown"
    fi
}

# Function to check if a package is installed (Debian/Ubuntu)
check_package_deb() {
    dpkg -l | grep -q "^ii  $1 " 2>/dev/null
}

# Function to check if a package is installed (Fedora/RHEL)
check_package_rpm() {
    rpm -q "$1" >/dev/null 2>&1
}

# Function to install missing dependencies
install_dependencies() {
    local distro=$(detect_distro)
    local missing_packages=()
    local dev_packages=()
    local cross_packages=()
    
    echo ""
    echo "Checking for required dependencies..."
    
    case "$distro" in
        ubuntu|debian)
            # Runtime dependencies
            if ! check_package_deb libasound2
            then
                missing_packages+=(libasound2)
            fi
            if ! check_package_deb libgl1-mesa-glx
            then
                missing_packages+=(libgl1-mesa-glx)
            fi
            if ! check_package_deb libx11-6
            then
                missing_packages+=(libx11-6)
            fi
            
            # Development dependencies
            if ! check_package_deb build-essential
            then
                dev_packages+=(build-essential)
            fi
            if ! check_package_deb pkg-config
            then
                dev_packages+=(pkg-config)
            fi
            if ! check_package_deb libasound2-dev
            then
                dev_packages+=(libasound2-dev)
            fi
            if ! check_package_deb libgl1-mesa-dev
            then
                dev_packages+=(libgl1-mesa-dev)
            fi
            if ! check_package_deb libx11-dev
            then
                dev_packages+=(libx11-dev)
            fi
            
            # Cross-compilation dependencies for ARM64
            if ! check_package_deb gcc-aarch64-linux-gnu
            then
                cross_packages+=(gcc-aarch64-linux-gnu)
            fi
            
            if [ ${#missing_packages[@]} -gt 0 ] || [ ${#dev_packages[@]} -gt 0 ] || [ ${#cross_packages[@]} -gt 0 ]
            then
                echo "Installing missing dependencies..."
                if [ ${#missing_packages[@]} -gt 0 ]
                then
                    echo "  Runtime packages: ${missing_packages[*]}"
                fi
                if [ ${#dev_packages[@]} -gt 0 ]
                then
                    echo "  Development packages: ${dev_packages[*]}"
                fi
                if [ ${#cross_packages[@]} -gt 0 ]
                then
                    echo "  Cross-compilation packages: ${cross_packages[*]}"
                fi
                sudo apt-get update
                sudo apt-get install -y "${missing_packages[@]}" "${dev_packages[@]}" "${cross_packages[@]}"
            else
                echo "✓ All required dependencies are installed"
            fi
            ;;
        fedora|rhel|centos)
            # Runtime dependencies
            if ! check_package_rpm alsa-lib
            then
                missing_packages+=(alsa-lib)
            fi
            if ! check_package_rpm mesa-libGL
            then
                missing_packages+=(mesa-libGL)
            fi
            if ! check_package_rpm libX11
            then
                missing_packages+=(libX11)
            fi
            
            # Development dependencies
            if ! check_package_rpm gcc
            then
                dev_packages+=(gcc)
            fi
            if ! check_package_rpm pkg-config
            then
                dev_packages+=(pkg-config)
            fi
                if ! check_package_rpm alsa-lib-devel
            then
                dev_packages+=(alsa-lib-devel)
            fi
            if ! check_package_rpm mesa-libGL-devel
            then
                dev_packages+=(mesa-libGL-devel)
            fi
            if ! check_package_rpm libX11-devel
            then
                dev_packages+=(libX11-devel)
            fi
            
            # Cross-compilation dependencies for ARM64
            if ! check_package_rpm gcc-aarch64-linux-gnu
            then
                cross_packages+=(gcc-aarch64-linux-gnu)
            fi
            
            if [ ${#missing_packages[@]} -gt 0 ] || [ ${#dev_packages[@]} -gt 0 ] || [ ${#cross_packages[@]} -gt 0 ]
            then
                echo "Installing missing dependencies..."
                if [ ${#missing_packages[@]} -gt 0 ]
                then
                    echo "  Runtime packages: ${missing_packages[*]}"
                fi
                if [ ${#dev_packages[@]} -gt 0 ]
                then
                    echo "  Development packages: ${dev_packages[*]}"
                fi
                if [ ${#cross_packages[@]} -gt 0 ]
                then
                    echo "  Cross-compilation packages: ${cross_packages[*]}"
                fi
                if command -v dnf >/dev/null 2>&1
                then
                    sudo dnf install -y "${missing_packages[@]}" "${dev_packages[@]}" "${cross_packages[@]}"
                else
                    sudo yum install -y "${missing_packages[@]}" "${dev_packages[@]}" "${cross_packages[@]}"
                fi
            else
                echo "✓ All required dependencies are installed"
            fi
            ;;
        *)
            echo "Warning: Unknown Linux distribution. Cannot auto-install dependencies."
            echo "Please install manually:"
            echo "  Ubuntu/Debian: sudo apt-get install -y libasound2 libgl1-mesa-glx libx11-6 build-essential pkg-config libasound2-dev libgl1-mesa-dev libx11-dev"
            echo "  Fedora/RHEL:   sudo dnf install -y alsa-lib mesa-libGL libX11 gcc pkg-config alsa-lib-devel mesa-libGL-devel libX11-devel"
            ;;
    esac
}

# Check and install dependencies before building
install_dependencies

# i18n merge + embedded_defaults sync: run on macOS (./compile-mac.sh or ./tools/i18n_sync_for_build.sh)
# before rsync so Linux/Windows builds do not require a modern Python on those hosts.

# fast update fyne before compile
go get fyne.io/fyne/v2@latest # or a specific version like @v2.4.0


# Build for AMD64
echo ""
echo "Building for Linux AMD64..."
AMD64_SUCCESS=false
set +e  # Temporarily disable exit-on-error to check build status
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o "bin/${KB_FPM_NAME}-linux-amd64"
AMD64_BUILD_EXIT=$?
set -e  # Re-enable exit-on-error
if [ $AMD64_BUILD_EXIT -eq 0 ]
then
    echo "✓ Linux AMD64 build successful"
    AMD64_SUCCESS=true
else
    echo "✗ Linux AMD64 build failed"
    AMD64_SUCCESS=false
fi

# Build for ARM64 (non-fatal - script continues even if this fails)
echo ""
echo "Building for Linux ARM64..."
ARM64_SUCCESS=false
# Check if cross-compiler is available
if command -v aarch64-linux-gnu-gcc >/dev/null 2>&1
then
    # Temporarily disable exit-on-error for ARM64 build
    set +e
    CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o "bin/${KB_FPM_NAME}-linux-arm64" 2>&1
    ARM64_BUILD_EXIT=$?
    set -e  # Re-enable exit-on-error
    if [ $ARM64_BUILD_EXIT -eq 0 ]
    then
        echo "✓ Linux ARM64 build successful"
        ARM64_SUCCESS=true
    else
        echo "✗ Linux ARM64 build failed (non-fatal, continuing...)"
        ARM64_SUCCESS=false
    fi
else
    echo "⚠ Cross-compiler (gcc-aarch64-linux-gnu) not found. Skipping ARM64 build."
    echo "  Install with: sudo apt-get install -y gcc-aarch64-linux-gnu"
fi

echo ""
echo "Done. Binaries:"
ls -lh "bin/${KB_FPM_NAME}"-linux-* 2>/dev/null || echo "  (no binaries found)"

# Exit with error only if AMD64 build failed (ARM64 failure is non-fatal)
if [ "$AMD64_SUCCESS" = false ]
then
    echo ""
    echo "✗ Build failed: AMD64 build did not succeed"
    exit 1
elif [ "$ARM64_SUCCESS" = false ] && command -v aarch64-linux-gnu-gcc >/dev/null 2>&1
then
    echo ""
    echo "⚠ Warning: ARM64 build failed, but AMD64 build succeeded. Continuing..."
fi

# Copy amd64 to current directory for easy testing (if it exists)
if [ -f "bin/${KB_FPM_NAME}-linux-amd64" ]
then
    cp "bin/${KB_FPM_NAME}-linux-amd64" "./${KB_LINUX_TEST_BINARY}"
fi

# Verify runtime dependencies after build
if command -v ldd >/dev/null 2>&1
then
    echo ""
    echo "Verifying runtime dependencies (ldd)..."
    if [ -f "bin/${KB_FPM_NAME}-linux-amd64" ]
    then
        echo "AMD64 binary:"
        ldd "bin/${KB_FPM_NAME}-linux-amd64" | grep -E "libasound|libGL|libX11" || true
    fi
    # Note: ldd won't work on ARM64 binary when running on x86_64
fi

echo ""
echo "Runtime requirements (not bundled):"
echo "- ALSA library (libasound2) — common Fyne/Linux dependency (this app does not play bundled sounds)"
echo "- OpenGL/X11 libs for Fyne (typically present on desktop distros)"
echo ""
echo "If dependencies are missing, they will be automatically installed."
echo "Manual install commands (for reference):"
echo "  Ubuntu/Debian: sudo apt-get install -y libasound2 libgl1-mesa-glx libx11-6"
echo "  Ubuntu/Debian: sudo apt-get install -y build-essential pkg-config libasound2-dev libgl1-mesa-dev libx11-dev"
echo "  Ubuntu/Debian: sudo apt-get install -y gcc-aarch64-linux-gnu (for ARM64 cross-compilation)"
echo "  Fedora/RHEL:   sudo dnf install -y alsa-lib mesa-libGL libX11"
echo "  Fedora/RHEL:   sudo dnf install -y gcc pkg-config alsa-lib-devel mesa-libGL-devel libX11-devel"
echo "  Fedora/RHEL:   sudo dnf install -y gcc-aarch64-linux-gnu (for ARM64 cross-compilation)"

# SCP retrieval function - copies binaries FROM this machine TO a remote location
# This is useful when running the script on a remote Ubuntu system and wanting to retrieve binaries
send_binaries() {
    # Check if we should send binaries to a remote location
    if [ -n "$SCP_DEST_HOST" ] && [ -n "$SCP_DEST_USER" ]
    then
        echo ""
        echo "Sending binaries via SCP to remote location..."
        echo "  Destination: $SCP_DEST_USER@$SCP_DEST_HOST"
        
        # Get the current directory name to construct destination path
        local current_dir=$(basename "$(pwd)")
        local dest_path="${SCP_DEST_PATH:-~/$current_dir/bin/}"
        
        echo "  Destination path: $dest_path"
        
        # Find all compiled binaries
        local binaries
        binaries=$(ls "bin/${KB_FPM_NAME}"-linux-* 2>/dev/null)
        
        if [ -z "$binaries" ]
        then
            echo "  ⚠ No binaries found to send"
            return
        fi
        
        # Create remote directory and copy binaries
        ssh "$SCP_DEST_USER@$SCP_DEST_HOST" "mkdir -p $dest_path" 2>/dev/null
        if scp $binaries "$SCP_DEST_USER@$SCP_DEST_HOST:$dest_path" 2>/dev/null
        then
            echo "✓ Successfully sent binaries to $SCP_DEST_USER@$SCP_DEST_HOST:$dest_path"
        else
            echo "⚠ Failed to send binaries via SCP"
            echo "  Make sure SSH keys are set up"
        fi
    elif [ -n "$SSH_CLIENT" ] || [ -n "$SSH_TTY" ]
    then
        # We're in an SSH session - provide instructions for retrieving binaries
        echo ""
        echo "To retrieve binaries from this remote system, run on your local machine:"
        local current_dir=$(basename "$(pwd)")
        local remote_user=$(whoami)
        local remote_host=$(hostname -f 2>/dev/null || hostname)
        echo "  scp $remote_user@$remote_host:$(pwd)/bin/${KB_FPM_NAME}-linux-* ./bin/"
        echo ""
        echo "Or set environment variables to auto-send:"
        echo "  export SCP_DEST_USER=your_local_username"
        echo "  export SCP_DEST_HOST=your_local_hostname_or_ip"
        echo "  export SCP_DEST_PATH=~/path/to/bin/  # optional, defaults to ~/$current_dir/bin/"
    fi
}

# Attempt to send binaries if SCP destination variables are set
send_binaries

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
