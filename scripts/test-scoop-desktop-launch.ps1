#Requires -Version 5.1

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scoopVersion = "v0.5.3"
$appName = "reasonix-desktop"
$appVersion = "0.0.0-ci"
$activeVersion = "v$appVersion"
$shortcutName = "Reasonix Desktop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$workRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("reasonix-scoop-launch-" + [guid]::NewGuid().ToString("N"))
$fixtureRoot = Join-Path $workRoot "fixture"
$packageRoot = Join-Path $fixtureRoot "package"
$scoopRoot = Join-Path $workRoot "scoop-root"
$scoopHome = Join-Path $scoopRoot "apps\scoop\current"
$manifestPath = Join-Path $fixtureRoot "$appName.json"
$archivePath = Join-Path $fixtureRoot "reasonix-windows-portable.zip"
$markerPath = Join-Path $workRoot "desktop-launched.json"
$shortcutPath = Join-Path ([Environment]::GetFolderPath("StartMenu")) "Programs\Scoop Apps\$shortcutName.lnk"
$server = $null

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(ValueFromRemainingArguments = $true)]
        [string[]]$ArgumentList
    )

    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed with exit code $LASTEXITCODE`: $FilePath $($ArgumentList -join ' ')"
    }
}

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$Content
    )

    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

try {
    if ($env:OS -ne "Windows_NT") {
        throw "This integration test must run on Windows."
    }
    if (Test-Path -LiteralPath $shortcutPath) {
        throw "Refusing to overwrite an existing shortcut: $shortcutPath"
    }

    New-Item -ItemType Directory -Force -Path (Join-Path $packageRoot "versions\$activeVersion") | Out-Null
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $scoopHome) | Out-Null

    $launcherPath = Join-Path $packageRoot "reasonix-launcher.exe"
    Push-Location $repoRoot
    try {
        Invoke-Native -FilePath "go" -ArgumentList @(
            "build", "-trimpath", "-ldflags", "-s -w -H windowsgui -X main.version=$appVersion",
            "-o", $launcherPath, "./cmd/reasonix-launcher"
        )
    } finally {
        Pop-Location
    }

    $fixtureSource = Join-Path $fixtureRoot "desktop-fixture.go"
    $desktopPath = Join-Path $packageRoot "versions\$activeVersion\reasonix-desktop.exe"
    Write-Utf8NoBom -Path $fixtureSource -Content @'
package main

import (
	"encoding/json"
	"os"
)

func main() {
	marker := os.Getenv("REASONIX_SCOOP_MARKER")
	executable, _ := os.Executable()
	workingDirectory, _ := os.Getwd()
	payload, _ := json.Marshal(map[string]any{
		"args":       os.Args[1:],
		"executable": executable,
		"workingDir": workingDirectory,
	})
	if marker == "" || os.WriteFile(marker, payload, 0o600) != nil {
		os.Exit(1)
	}
}
'@
    Invoke-Native -FilePath "go" -ArgumentList @(
        "build", "-trimpath", "-ldflags", "-s -w -H windowsgui", "-o", $desktopPath, $fixtureSource
    )

    Write-Utf8NoBom -Path (Join-Path $packageRoot "current.json") -Content @"
{
  "schemaVersion": 1,
  "activeVersion": "$activeVersion",
  "activeDir": "versions/$activeVersion"
}
"@

    Compress-Archive -Path (Join-Path $packageRoot "*") -DestinationPath $archivePath -CompressionLevel Optimal
    $archiveHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()

    $listener = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    $port = ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
    $listener.Stop()

    $python = Get-Command python -ErrorAction Stop
    $serverOut = Join-Path $workRoot "http-server.stdout.log"
    $serverErr = Join-Path $workRoot "http-server.stderr.log"
    $server = Start-Process -FilePath $python.Source -ArgumentList @(
        "-m", "http.server", $port, "--bind", "127.0.0.1", "--directory", $fixtureRoot
    ) -RedirectStandardOutput $serverOut -RedirectStandardError $serverErr -PassThru

    $archiveUrl = "http://127.0.0.1:$port/$(Split-Path -Leaf $archivePath)"
    $ready = $false
    for ($attempt = 0; $attempt -lt 40; $attempt++) {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $archiveUrl -Method Head -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                $ready = $true
                break
            }
        } catch {
            Start-Sleep -Milliseconds 250
        }
    }
    if (!$ready) {
        throw "Fixture HTTP server did not become ready."
    }

    Write-Utf8NoBom -Path $manifestPath -Content @"
{
  "version": "$appVersion",
  "description": "Reasonix Scoop integration fixture",
  "homepage": "https://github.com/esengine/DeepSeek-Reasonix",
  "license": "Apache-2.0",
  "url": "$archiveUrl",
  "hash": "$archiveHash",
  "shortcuts": [
    ["reasonix-launcher.exe", "$shortcutName", "launch --detach"]
  ]
}
"@

    Invoke-Native -FilePath "git" -ArgumentList @(
        "clone", "--branch", $scoopVersion, "--depth", "1", "https://github.com/ScoopInstaller/Scoop.git", $scoopHome
    )
    $env:SCOOP = $scoopRoot
    $env:SCOOP_CACHE = Join-Path $workRoot "scoop-cache"
    $env:REASONIX_SCOOP_MARKER = $markerPath
    $scoopCommand = Join-Path $scoopHome "bin\scoop.ps1"
    Invoke-Native -FilePath "powershell.exe" -ArgumentList @(
        "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $scoopCommand,
        "install", "--no-update-scoop", $manifestPath
    )

    $currentPath = Join-Path $scoopRoot "apps\$appName\current"
    $currentItem = Get-Item -LiteralPath $currentPath -Force
    if (($currentItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) {
        throw "Scoop current path is not a directory junction: $currentPath"
    }

    if (!(Test-Path -LiteralPath $shortcutPath -PathType Leaf)) {
        throw "Scoop did not create the expected shortcut: $shortcutPath"
    }
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $expectedTarget = Join-Path $currentPath "reasonix-launcher.exe"
    if (![string]::Equals($shortcut.TargetPath, $expectedTarget, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Unexpected shortcut target. Expected '$expectedTarget', got '$($shortcut.TargetPath)'."
    }
    if ($shortcut.Arguments -ne "launch --detach") {
        throw "Unexpected shortcut arguments: '$($shortcut.Arguments)'."
    }

    Start-Process -FilePath $shortcutPath
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while (!(Test-Path -LiteralPath $markerPath) -and [DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Milliseconds 250
    }
    if (!(Test-Path -LiteralPath $markerPath)) {
        throw "The desktop fixture was not launched through the Scoop shortcut."
    }

    $launch = Get-Content -LiteralPath $markerPath -Raw | ConvertFrom-Json
    $versionPath = Join-Path $scoopRoot "apps\$appName\$appVersion"
    $expectedDesktop = Join-Path $versionPath "versions\$activeVersion\reasonix-desktop.exe"
    if (![string]::Equals($launch.executable, $expectedDesktop, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Unexpected desktop executable. Expected '$expectedDesktop', got '$($launch.executable)'."
    }
    if (![string]::Equals($launch.workingDir, $versionPath, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Unexpected launcher working directory. Expected '$versionPath', got '$($launch.workingDir)'."
    }
    if (@($launch.args).Count -ne 0) {
        throw "Legacy shortcut arguments reached the desktop fixture: $($launch.args -join ' ')"
    }

    Write-Host "Scoop install, current junction, shortcut, and desktop launch verified."
} finally {
    if ($server -and !$server.HasExited) {
        Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $shortcutPath) {
        Remove-Item -LiteralPath $shortcutPath -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $workRoot) {
        Remove-Item -LiteralPath $workRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}
