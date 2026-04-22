#!/usr/bin/env bash

# prepare-deps.sh - Prepare Go dependencies for KrankyBear FileMover
# This script downloads modules into the Go module cache and tidies go.mod (no ./vendor).

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}KrankyBear FileMover - Dependency Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed or not in PATH${NC}"
    echo "Please install Go from https://golang.org/dl/"
    exit 1
fi

# Display Go version
GO_VERSION=$(go version | awk '{print $3}')
echo -e "${GREEN}✓${NC} Found Go: ${GO_VERSION}"
echo ""

# Check if go.mod exists
if [ ! -f "go.mod" ]; then
    echo -e "${RED}Error: go.mod not found in current directory${NC}"
    exit 1
fi

echo -e "${YELLOW}Step 1:${NC} Downloading all dependencies..."
echo "Running: go mod download"
if go mod download; then
    echo -e "${GREEN}✓${NC} Dependencies downloaded successfully"
else
    echo -e "${RED}✗${NC} Failed to download dependencies"
    exit 1
fi
echo ""

echo -e "${YELLOW}Step 2:${NC} Tidying module dependencies..."
echo "Running: go mod tidy"
if go mod tidy; then
    echo -e "${GREEN}✓${NC} Module dependencies tidied"
else
    echo -e "${RED}✗${NC} Failed to tidy dependencies"
    exit 1
fi
echo ""

echo -e "${YELLOW}Step 3:${NC} Verifying module dependencies..."
echo "Running: go mod verify"
if go mod verify; then
    echo -e "${GREEN}✓${NC} Module dependencies verified"
else
    echo -e "${YELLOW}⚠${NC} Module verification had issues (this may be normal)"
fi
echo ""

# Count dependencies (handle errors gracefully)
# Count direct dependencies (lines with require that don't have // indirect)
DIRECT_DEPS=$(grep -E "^require " go.mod 2>/dev/null | grep -v "// indirect" | wc -l | tr -d ' ' || echo "0")
# Also count dependencies in require() blocks without // indirect
DIRECT_IN_BLOCK=$(awk '/^require \(/,/^\)/ {if ($0 !~ /\/\/ indirect/ && $0 !~ /^require/ && $0 !~ /^\)/ && NF > 0) count++} END {print count+0}' go.mod 2>/dev/null || echo "0")
DIRECT_DEPS=$((DIRECT_DEPS + DIRECT_IN_BLOCK))
INDIRECT_DEPS=$(grep -c "// indirect" go.mod 2>/dev/null || echo "0")
GOMODCACHE=$(go env GOMODCACHE 2>/dev/null || echo "")

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Dependency Setup Complete!${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo "Summary:"
echo "  • Direct dependencies: ${DIRECT_DEPS}"
echo "  • Indirect dependencies: ${INDIRECT_DEPS}"
echo "  • Module cache: ${GOMODCACHE:-<unset>}"
echo ""
echo -e "${GREEN}You can now build the application with:${NC}"
echo "  make build"
echo "  or"
echo "  go build -o filemover"
echo ""

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
