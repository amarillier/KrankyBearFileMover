# prepare-deps.ps1 - Prepare Go dependencies (Windows)
# Populates the module cache via go mod download / tidy / verify — no ./vendor tree.
# Uses ASCII-only output for Windows PowerShell 5.1 + UTF-8 scripts without BOM.

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "KrankyBear FileMover - Dependency Setup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Host "Error: Go is not installed or not in PATH" -ForegroundColor Red
    Write-Host "Please install Go from https://golang.org/dl/" -ForegroundColor Yellow
    exit 1
}

$goVersion = & go version
Write-Host "OK Found Go: $goVersion" -ForegroundColor Green
Write-Host ""

if (-not (Test-Path "go.mod")) {
    Write-Host "Error: go.mod not found in current directory" -ForegroundColor Red
    exit 1
}

$env:GOWORK = "off"
$gf = $env:GOFLAGS
if ($gf -and $gf -match "-mod=vendor") {
    Write-Host "NOTE: Replacing -mod=vendor in GOFLAGS with -mod=mod (no ./vendor after sync)." -ForegroundColor Yellow
    $gf = $gf -replace "-mod=vendor", "-mod=mod"
    $env:GOFLAGS = $gf
}

Write-Host "Step 1: Downloading all dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod download"
try {
    & go mod download
    if ($LASTEXITCODE -eq 0) {
        Write-Host "OK Dependencies downloaded successfully" -ForegroundColor Green
    } else {
        Write-Host "[FAIL] Failed to download dependencies" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "[FAIL] Failed to download dependencies: $_" -ForegroundColor Red
    exit 1
}
Write-Host "Running: go mod download github.com/go-gl/gl (Fyne / Windows desktop)"
try {
    & go mod download github.com/go-gl/gl github.com/go-gl/glfw/v3.3/glfw
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[FAIL] go-gl download failed" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "[FAIL] go-gl download failed: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "Step 2: Tidying module dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod tidy"
try {
    & go mod tidy
    if ($LASTEXITCODE -eq 0) {
        Write-Host "OK Module dependencies tidied" -ForegroundColor Green
    } else {
        Write-Host "[FAIL] Failed to tidy dependencies" -ForegroundColor Red
        exit 1
    }
} catch {
    Write-Host "[FAIL] Failed to tidy dependencies: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

Write-Host "Step 3: Verifying module dependencies..." -ForegroundColor Yellow
Write-Host "Running: go mod verify"
try {
    & go mod verify
    if ($LASTEXITCODE -eq 0) {
        Write-Host "OK Module dependencies verified" -ForegroundColor Green
    } else {
        Write-Host "[WARN] Module verification had issues (this may be normal)" -ForegroundColor Yellow
    }
} catch {
    Write-Host "[WARN] Module verification had issues (this may be normal)" -ForegroundColor Yellow
}
Write-Host ""

$directDeps = 0
$indirectDeps = 0

try {
    $requireLines = Select-String -Path "go.mod" -Pattern "^\s+\S+" -ErrorAction SilentlyContinue
    if ($requireLines) {
        $directDeps = @($requireLines | Where-Object { $_.Line -notmatch "// indirect" }).Count
    }
} catch {
    $directDeps = 0
}

try {
    $indirectLines = Select-String -Path "go.mod" -Pattern "// indirect" -ErrorAction SilentlyContinue
    if ($indirectLines) {
        $indirectDeps = @($indirectLines).Count
    }
} catch {
    $indirectDeps = 0
}

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Dependency Setup Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Summary:"
Write-Host "  - Direct dependencies: $directDeps"
Write-Host "  - Indirect dependencies: $indirectDeps"
Write-Host "  - Module cache: $(go env GOMODCACHE)"
Write-Host ""
Write-Host "You can now build with compile-windows.ps1 or:" -ForegroundColor Green
Write-Host '  go build -ldflags "-s -w -H windowsgui" -trimpath -o bin\KrankyBearFileMover.exe .'
Write-Host ""

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
