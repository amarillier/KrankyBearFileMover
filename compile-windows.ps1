param(
    [switch]$Windows,
    [switch]$Package,
    [string]$InnoPath = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe'
)

$ErrorActionPreference = 'Continue'

function Log([string]$msg) {
    Write-Host $msg
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($scriptDir) {
    Set-Location $scriptDir
}

. (Join-Path $scriptDir 'build-config.ps1')

Log ($KB_PROJECT_TITLE + ' - Windows compile (GUI, native Windows host)')
Log '============================================================='
Log ('Working directory: ' + (Get-Location).Path)
Log ''

if (-not (Test-Path 'bin')) {
    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
}

Remove-Item -Path ('bin\' + $KB_WINDOWS_EXE) -Force -ErrorAction SilentlyContinue
Get-ChildItem -Path $scriptDir -Filter '*.syso' -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Log 'ERROR: go not found in PATH.'
    exit 1
}

if (-not (Test-Path 'go.mod')) {
    Log 'ERROR: go.mod not found in working directory (wrong folder?).'
    exit 1
}

$goModText = Get-Content -Path 'go.mod' -Raw -ErrorAction SilentlyContinue
if (-not $goModText -or $goModText -notmatch 'github\.com/go-gl/gl' -or $goModText -notmatch 'fyne\.io/fyne/v2') {
    Log 'ERROR: go.mod looks stale or wrong (missing fyne / go-gl lines).'
    Log 'If you use sync2windows.sh, KB_WINDOWS_SRC_WIN / compile path must be the tree that receives the share sync, not an old copy.'
    exit 1
}

# A parent go.work on the machine can change the module graph; non-interactive SSH often differs from desktop.
$env:GOWORK = 'off'

# sync2windows.sh excludes vendor/. Machine or user GOFLAGS with -mod=vendor then yields
# "no required module provides ... github.com/go-gl/gl/v2.1/gl" even though go.mod lists the module.
$gf = $env:GOFLAGS
if ($gf -and $gf -match '-mod=vendor') {
    Log 'NOTE: Replacing -mod=vendor in GOFLAGS with -mod=mod (this project builds from module cache).'
    $gf = $gf -replace '-mod=vendor', '-mod=mod'
}
# Disable VCS stamping so builds succeed when .git is missing or restricted (same idea as TaniumSensorExplorer).
$env:GOFLAGS = if ($gf) { "$gf -buildvcs=false" } else { '-buildvcs=false' }

# OpenSSH (especially from macOS/Linux) can forward AcceptEnv and inject GOOS/GOARCH/CGO from the client.
# That breaks Windows desktop builds and can yield "no required module provides ... github.com/go-gl/gl/v2.1/gl"
# because module/tooling sees the wrong platform. Interactive desktop logins usually do not set these.
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '1'

Log ('Diagnostics: ' + (go version))
Log 'Diagnostics: go env GOMOD GOWORK GOFLAGS GOOS GOARCH CGO_ENABLED'
& go env GOMOD GOWORK GOFLAGS GOOS GOARCH CGO_ENABLED
Log ''

# Dependencies resolve from the module cache (go env GOMODCACHE), not ./vendor.
# Do not run "go get fyne.io/fyne/v2@latest" here: it mutates go.mod/go.sum away from the repo pin and
# leaves a broken graph until "go mod tidy" (as in prepare-deps.ps1). Use committed go.mod only.
go mod download
if ($LASTEXITCODE -ne 0) {
    Log 'ERROR: go mod download failed.'
    exit 1
}

# Fyne's GLFW path imports github.com/go-gl/gl/v2.1/gl; explicit download helps after partial cache copies.
Log 'Caching OpenGL modules (go-gl)...'
go mod download github.com/go-gl/gl github.com/go-gl/glfw/v3.3/glfw
if ($LASTEXITCODE -ne 0) {
    Log 'ERROR: go mod download github.com/go-gl/gl failed.'
    exit 1
}

# i18n sync runs on macOS (compile-mac.sh). Embedded English should already be in repo after sync from Mac.

if (Get-Command go-winres -ErrorAction SilentlyContinue) {
    if ((Test-Path $KB_WINRES_ICON64) -and (Test-Path $KB_WINRES_ICON_MAIN)) {
        go-winres make -arch amd64
    } else {
        Log 'WARNING: icon files for go-winres are missing; continuing.'
    }
} else {
    Log 'WARNING: go-winres not found; continuing without icon resource generation.'
}

Remove-Item Env:\CC -ErrorAction SilentlyContinue
if (Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue) {
    $env:CC = 'x86_64-w64-mingw32-gcc'
}
Get-ChildItem -Path $scriptDir -Filter '*386.syso' -Recurse -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

$buildFailed = $false

Log ('Building Windows GUI binary (bin\' + $KB_WINDOWS_EXE + ')...')
go build -mod=mod -ldflags '-s -w -H windowsgui' -trimpath -o ('bin\' + $KB_WINDOWS_EXE) .
if ($LASTEXITCODE -ne 0) {
    Log 'ERROR: Windows build failed.'
    $buildFailed = $true
} else {
    Log ('OK: bin\' + $KB_WINDOWS_EXE)
}

if ($Package -and -not $buildFailed) {
    if (-not (Test-Path ('bin\' + $KB_WINDOWS_EXE))) {
        Log ('ERROR: bin\' + $KB_WINDOWS_EXE + ' not found for packaging.')
        $buildFailed = $true
    } elseif (-not (Test-Path $InnoPath)) {
        Log ('ERROR: Inno Setup not found at: ' + $InnoPath)
        $buildFailed = $true
    } else {
        Log 'Running Inno Setup packaging...'
        & $InnoPath $KB_INNO_ISS
        if ($LASTEXITCODE -ne 0) {
            Log 'ERROR: Inno Setup packaging failed.'
            $buildFailed = $true
        } else {
            Log 'Inno Setup packaging succeeded.'
        }
    }
}

Log ''
Log '============================================================='
if ($buildFailed) {
    Log 'One or more build steps failed.'
    exit 1
}

Log 'Compile complete.'
Get-ChildItem -Path 'bin' -Filter $KB_WINDOWS_EXE -ErrorAction SilentlyContinue | Format-Table Name, Length -AutoSize
