# Windows compile script for KrankyBearFileMover / filemover-windows (Windows build + optional packaging)
# Note: Remove Unix shebang for Windows execution via SSH

param(
    [switch]$Windows,
    [switch]$Linux,
    [switch]$All,
    [switch]$Current,
    [switch]$Package,              # After Windows build, run Inno Setup to create installer
    [string]$InnoPath = "C:\Program Files (x86)\Inno Setup 6\ISCC.exe"  # Path to ISCC.exe
)

# PowerShell execution policy and error handling (after param block)
$ErrorActionPreference = "Continue"  # Continue on errors so we can report them

# Get script directory and change to it (important for SSH execution)
$PSScriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($PSScriptRoot) {
    Set-Location $PSScriptRoot
    Write-Host "Changed to script directory: $PSScriptRoot" -ForegroundColor Gray
} else {
    $PSScriptRoot = $PWD.Path
}

Write-Host "KrankyBear FileMover - Windows Compile Script" -ForegroundColor Cyan
Write-Host "================================================" -ForegroundColor Cyan
Write-Host "Working directory: $PWD" -ForegroundColor Gray
Write-Host "" 

# Create bin directory if it doesn't exist
$binDir = "bin"
if (-not (Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
}

# Cleanup previous Windows/Linux binaries only (keep Resources/ and other assets)
Remove-Item -Path (Join-Path bin 'filemover') -Force -ErrorAction SilentlyContinue

# Remove ALL syso files before generating new ones with go-winres
# This ensures we don't use stale/cached icons (e.g., KrankyBearBeret)
# Both old rsrc tool files and go-winres files need to be removed
Get-ChildItem -Path $PSScriptRoot -Filter "*.syso" -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
Write-Host "Cleaned up all existing syso files (if any existed)" -ForegroundColor Gray

# Check if Go is installed
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Error: Go is not installed. Please install Go 1.21 or later." -ForegroundColor Red
    exit 1
}

# Check for module and other dependencies
if (-not (Test-Path "vendor/modules.txt")) {
    Write-Host "Error: vendor/modules.txt not found. Running prepare-deps.ps1 first which may take a while." -ForegroundColor Red
    .\prepare-deps.ps1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ prepare-deps.ps1 completed successfully" -ForegroundColor Green
    } else {
        Write-Host "✗ prepare-deps.ps1 failed" -ForegroundColor Red
        exit 1
    }
}

# Display Go version
$goVersion = go version
Write-Host "Using: $goVersion" -ForegroundColor Green
Write-Host ""

# fast update fyne before compile
go get fyne.io/fyne/v2@latest # or a specific version like @v2.4.0
go mod tidy
go mod vendor

# Verify winres make is installed and run
go install github.com/tc-hib/go-winres@latest
# Generate Windows resources (icon, version info, manifest)
# go-winres must be run from root directory - it looks for winres/winres.json
Write-Host "Generating Windows resources from winres/winres.json..." -ForegroundColor Cyan
go-winres make -arch amd64
if ($LASTEXITCODE -ne 0) {
    Write-Host "WARNING: go-winres failed. Icon may not be embedded." -ForegroundColor Yellow
    Write-Host "Install with: go install github.com/tc-hib/go-winres@latest" -ForegroundColor Yellow
    Write-Host "Verify winres/winres.json references the correct icon file." -ForegroundColor Yellow
    Write-Host "Current icon reference: ../Resources/Images/KrankyBearCowboyBrown.png" -ForegroundColor Yellow
    Write-Host ""
} else {
    Write-Host "✓ Windows resources generated successfully" -ForegroundColor Green
    # Verify syso file was created
    $sysoFiles = Get-ChildItem -Path $PSScriptRoot -Filter "*.syso" -ErrorAction SilentlyContinue
    if ($sysoFiles) {
        Write-Host "Created syso files:" -ForegroundColor Gray
        $sysoFiles | ForEach-Object { Write-Host "  $($_.Name) ($([math]::Round($_.Length/1KB, 2)) KB)" -ForegroundColor Gray }
    } else {
        Write-Host "WARNING: No syso files found after go-winres make" -ForegroundColor Yellow
        Write-Host "The build will proceed but may use cached/old icon resources." -ForegroundColor Yellow
    }
}

# Ensure 64-bit build environment
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "1"
# Prefer a 64-bit MinGW if available
if (Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue) {
    $env:CC = "x86_64-w64-mingw32-gcc"
}
# Clear any stale 32-bit cache artifacts - only clean cache if needed, or it slows builds
# go clean -cache -testcache -i | Out-Null
# Remove any stray 32-bit resource objects in the tree
Get-ChildItem -Path $PSScriptRoot -Filter "*386.syso" -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

$buildFailed = $false

if ($All -or $Windows -or (-not $Linux -and -not $Current)) {
    Write-Host "Building for Windows..." -ForegroundColor Yellow
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    # Always build as GUI app (no console window)
    $ldflags = "-s -w -H windowsgui"
    go build -ldflags="$ldflags" -trimpath -o bin/filemover-windows.exe
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Windows build successful" -ForegroundColor Green
        if ($Package) {
            Write-Host "Packaging Windows installer with Inno Setup..." -ForegroundColor Yellow
            # Copy/rename to match Inno script expectation
            try {
                Copy-Item -Path (Join-Path $PSScriptRoot "bin/filemover-windows.exe") -Destination (Join-Path $PSScriptRoot "filemover-windows.exe") -Force
            } catch {
                Write-Host "Failed to copy Windows binary for packaging: $_" -ForegroundColor Red
                $buildFailed = $true
            }

            if (Test-Path $InnoPath) {
                & "$InnoPath" "Inno/KrankyBearFileMover.iss"
                if ($LASTEXITCODE -eq 0) {
                    Write-Host "✓ Inno Setup packaging complete (see installers/ folder)" -ForegroundColor Green
                } else {
                    Write-Host "✗ Inno Setup packaging failed (exit $LASTEXITCODE)" -ForegroundColor Red
                    $buildFailed = $true
                }
            } else {
                Write-Host "Inno Setup not found at: $InnoPath" -ForegroundColor Red
                Write-Host "Install Inno Setup 6 and/or pass -InnoPath to this script." -ForegroundColor Yellow
                $buildFailed = $true
            }
        }
    } else {
        Write-Host "✗ Windows build failed" -ForegroundColor Red
        $buildFailed = $true
    }
    Write-Host ""
}

if ($All -or $Linux) {
    if ($env:OS -eq 'Windows_NT') {
        Write-Host "Skipping Linux build on Windows (CGO/OpenGL/ALSA cross-compile unsupported)." -ForegroundColor Yellow
        Write-Host "Build Linux on a Linux host using ./compile-linux.sh or sync2ubuntu18.sh" -ForegroundColor Yellow
    } else {
        Write-Host "Building for Linux..." -ForegroundColor Yellow
        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        go build -ldflags="-s -w" -trimpath -o bin/filemover-linux
        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ Linux build successful" -ForegroundColor Green
        } else {
            Write-Host "✗ Linux build failed" -ForegroundColor Red
            $buildFailed = $true
        }
        Write-Host ""
    }
}

if ($Current) {
    Write-Host "Building for current platform..." -ForegroundColor Yellow
    go build -ldflags="-s -w" -trimpath -o bin/filemover-windows
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Current platform build successful" -ForegroundColor Green
    } else {
        Write-Host "✗ Current platform build failed" -ForegroundColor Red
        $buildFailed = $true
    }
    Write-Host ""
}

Write-Host "================================================" -ForegroundColor Cyan
if ($buildFailed) {
    Write-Host "One or more build steps failed." -ForegroundColor Red
    exit 1
} else {
    Write-Host "Compile complete! Binaries are in the bin/ directory." -ForegroundColor Green
    Get-ChildItem -Path bin -Filter "*filemover*" | Format-Table Name, Length -AutoSize
    
    # Offer to clear icon cache if Windows build was successful
    if ($All -or $Windows -or (-not $Linux -and -not $Current)) {
        Write-Host ""
        Write-Host "Note: If the executable shows the old icon, Windows may be caching it." -ForegroundColor Yellow
        Write-Host "To clear the icon cache, run this command as Administrator:" -ForegroundColor Yellow
        Write-Host "  ie4uinit.exe -ClearIconCache" -ForegroundColor Cyan
        Write-Host ""
        Write-Host "Or manually:" -ForegroundColor Yellow
        Write-Host "  1. Task Manager > End 'Windows Explorer' process" -ForegroundColor Yellow
        Write-Host "  2. Delete iconcache*.db from %localappdata%\Microsoft\Windows\Explorer" -ForegroundColor Yellow
        Write-Host "  3. Run 'explorer.exe' to restart Windows Explorer" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "To verify the embedded icon, extract it with:" -ForegroundColor Yellow
        Write-Host "  go-winres extract bin\filemover-windows.exe --dir extracted_resources" -ForegroundColor Cyan
    }
}

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
