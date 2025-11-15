# prepare-deps.ps1 - Prepare Go dependencies for KrankyBear LaunchPad (Windows)
# This script downloads all required packages, tidies the module, and creates a vendor directory
# for efficient first-time compilation.

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "KrankyBear FileMover - Dependency Setup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if Go is installed
$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Host "Error: Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from https://golang.org/dl/" -ForegroundColor Yellow
    exit 1
}

# Display Go version
$goVersion = & go version
Write-Host "✓ Found Go: $goVersion" -ForegroundColor Green
Write-Host ""

# Check if go.mod exists
if (-not (Test-Path "go.mod")) {
    Write-Host "Error: go.mod not found in current directory" -ForegroundColor Red
    exit 1
}

Write-Host "Step 1: Downloading all dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod download"
try {
    & go mod download
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Dependencies downloaded successfully" -ForegroundColor Green
    } else {
        Write-Host "✗ Failed to download dependencies" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "✗ Failed to download dependencies: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "Step 2: Tidying module dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod tidy"
try {
    & go mod tidy
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Module dependencies tidied" -ForegroundColor Green
    } else {
        Write-Host "✗ Failed to tidy dependencies" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "✗ Failed to tidy dependencies: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "Step 3: Verifying module dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod verify"
try {
    & go mod verify
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Module dependencies verified" -ForegroundColor Green
    } else {
        Write-Host "⚠ Module verification had issues (this may be normal)" -ForegroundColor Yellow
    }
} catch {
    Write-Host "⚠ Module verification had issues (this may be normal)" -ForegroundColor Yellow
}
Write-Host ""

Write-Host "Step 4: Creating vendor directory..." -ForegroundColor Yellow
Write-Host "Running: go mod vendor"
try {
    & go mod vendor
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Vendor directory created successfully" -ForegroundColor Green
    } else {
        Write-Host "✗ Failed to create vendor directory" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "✗ Failed to create vendor directory: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

# Count dependencies
$directDeps = (Select-String -Path "go.mod" -Pattern "^\s+[^/]+" | Where-Object { $_.Line -notmatch "// indirect" }).Count
$indirectDeps = (Select-String -Path "go.mod" -Pattern "// indirect").Count
$vendorCount = (Get-ChildItem -Path "vendor" -Directory -Recurse -ErrorAction SilentlyContinue | Measure-Object).Count

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Dependency Setup Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Summary:"
Write-Host "  • Direct dependencies: $directDeps"
Write-Host "  • Indirect dependencies: $indirectDeps"
Write-Host "  • Vendor packages: $vendorCount"
Write-Host ""
Write-Host "You can now build the application with:" -ForegroundColor Green
Write-Host "  go build -mod=vendor -o filemover.exe"
Write-Host ""

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
