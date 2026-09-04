#!/usr/bin/env pwsh
# Windows twin of run-studio.sh: build the SPA, build the kernel the shell
# speaks to over loopback, then open the window. -SkipBuild launches what is
# already in the tree, which is the common case between two edits.
[CmdletBinding()]
param(
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $PSCommandPath
$frontend = Join-Path (Split-Path -Parent $here) "frontend-next"

# A failing native command sets $LASTEXITCODE rather than raising, so
# ErrorActionPreference alone would carry a broken build into the launch.
function Invoke-Pnpm {
    param(
        [Parameter(Mandatory = $true)][string]$Directory,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )

    Push-Location $Directory
    try {
        & pnpm @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "pnpm $($Arguments -join ' ') failed in $Directory (exit $LASTEXITCODE)"
        }
    } finally {
        Pop-Location
    }
}

if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
    throw "pnpm not found; install it with: npm install -g pnpm@10"
}

if (-not (Test-Path (Join-Path $here "node_modules"))) {
    Write-Host "==> deps"
    Invoke-Pnpm -Directory $here -Arguments @("install", "--frozen-lockfile")
}

if ($SkipBuild) {
    # The two paths src/layout.js resolves in a dev tree. Missing, they surface
    # as a spawn error or a blank window, so say which one is absent instead.
    $page = Join-Path $frontend "dist"
    $host_ = Join-Path $here "bin\reasonix-studio-host.exe"
    foreach ($required in @($page, $host_)) {
        if (-not (Test-Path $required)) {
            throw "$required is missing; run this script without -SkipBuild"
        }
    }
} else {
    Write-Host "==> frontend"
    Invoke-Pnpm -Directory $frontend -Arguments @("install", "--frozen-lockfile")
    Invoke-Pnpm -Directory $frontend -Arguments @("build")

    Write-Host "==> kernel"
    Invoke-Pnpm -Directory $here -Arguments @("build:host")
}

Write-Host "==> studio"
Invoke-Pnpm -Directory $here -Arguments @("start")
