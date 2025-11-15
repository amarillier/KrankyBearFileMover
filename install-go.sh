#!/bin/bash

# Install or update Go for Linux systems
# Define installation directory
INSTALL_DIR="/usr/local"
GO_INSTALL_PATH="${INSTALL_DIR}/go"

# Function to get the latest Go version
get_latest_go_version() {
    curl -s https://go.dev/dl/ | grep -oP 'go[0-9]+\.[0-9]+(\.[0-9]+)?\.linux-amd64\.tar\.gz' | head -n 1 | grep -oP 'go[0-9]+\.[0-9]+(\.[0-9]+)?'
}

# Function to check if Go is already installed
is_go_installed() {
    if command -v go &> /dev/null
    then
        return 0 # Go is installed
    else
        return 1 # Go is not installed
    fi
}

# Function to get the currently installed Go version
get_current_go_version() {
    if is_go_installed
    then
        go version | awk '{print $3}' | sed 's/go//'
    else
        echo "0.0.0" # Indicate no Go version installed
    fi
}

# Get latest and current Go versions
LATEST_VERSION=$(get_latest_go_version)
CURRENT_VERSION=$(get_current_go_version)

echo "Latest Go version available: ${LATEST_VERSION}"
echo "Currently installed Go version: ${CURRENT_VERSION}"

# Compare versions and decide if an update is needed
if [[ "$(printf '%s\n' "$LATEST_VERSION" "$CURRENT_VERSION" | sort -V | head -n 1)" = "$LATEST_VERSION" && "$LATEST_VERSION" != "$CURRENT_VERSION" ]]
then
    echo "A newer version of Go is available. Proceeding with update."
elif [[ "$LATEST_VERSION" == "$CURRENT_VERSION" ]]
then
    echo "You are already running the latest Go version. Exiting."
    exit 0
else
    echo "Installing Go for the first time."
fi

# Download and install
DOWNLOAD_URL="https://go.dev/dl/${LATEST_VERSION}.linux-amd64.tar.gz"
TEMP_FILE="/tmp/${LATEST_VERSION}.linux-amd64.tar.gz"

echo "Downloading Go from: ${DOWNLOAD_URL}"
wget -q "${DOWNLOAD_URL}" -O "${TEMP_FILE}"

if [ $? -ne 0 ]
then
    echo "Failed to download Go. Exiting."
    exit 1
fi

echo "Removing existing Go installation (if any)..."
sudo rm -rf "${GO_INSTALL_PATH}"

echo "Extracting and installing Go..."
sudo tar -C "${INSTALL_DIR}" -xzf "${TEMP_FILE}"

if [ $? -ne 0 ]
then
    echo "Failed to extract Go. Exiting."
    exit 1
fi

echo "Cleaning up temporary file..."
rm "${TEMP_FILE}"

# Set up environment variables if not already present
if ! grep -q 'export PATH=$PATH:/usr/local/go/bin' ~/.bashrc
then
    echo "Adding Go to PATH in ~/.bashrc"
    echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    source ~/.bashrc
fi

echo "Go ${LATEST_VERSION} installed successfully."
go version
