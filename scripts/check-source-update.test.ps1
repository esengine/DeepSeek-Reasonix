[CmdletBinding()]
param(
    [string]$AdapterPath = (Join-Path $PSScriptRoot 'check-source-update.ps1'),
    [string]$EvidenceRoot = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Invoke-AdapterCase {
    param(
        [Parameter(Mandatory = $true)][string]$CliPath,
        [Parameter(Mandatory = $true)][string]$SourceRoot,
        [Parameter(Mandatory = $true)][string]$ResultPath
    )

    $output = @(& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $AdapterPath `
        -CliPath $CliPath -SourceRoot $SourceRoot -ResultPath $ResultPath 2>&1)
    return [pscustomobject]@{
        ExitCode = [int]$LASTEXITCODE
        Output = $output -join [Environment]::NewLine
    }
}

function Assert-Equal {
    param(
        [Parameter(Mandatory = $true)]$Actual,
        [Parameter(Mandatory = $true)]$Expected,
        [Parameter(Mandatory = $true)][string]$Label
    )

    if ($Actual -ne $Expected) {
        throw "$Label = $Actual; expected $Expected"
    }
}

if (-not (Test-Path -LiteralPath $AdapterPath -PathType Leaf)) {
    throw "Source-update adapter is missing: $AdapterPath"
}
if ([string]::IsNullOrWhiteSpace($EvidenceRoot)) {
    $EvidenceRoot = Join-Path ([IO.Path]::GetTempPath()) 'reasonix-source-update-adapter-test'
}

$runRoot = Join-Path $EvidenceRoot (Get-Date -Format 'yyyyMMdd-HHmmss-fff')
$sourceRoot = Join-Path $runRoot 'source'
New-Item -ItemType Directory -Force -Path $runRoot, $sourceRoot | Out-Null

$fakeSource = @'
using System;

public static class FakeSourceUpdateCli
{
    private static void WriteResult(string status, string localBase, string remoteHead, string message)
    {
        Console.WriteLine("{\"sourceRoot\":\"fake\",\"branch\":\"test\",\"head\":\"head\",\"localBase\":\"" + localBase + "\",\"remoteUrl\":\"https://github.com/esengine/DeepSeek-Reasonix.git\",\"remoteRef\":\"refs/heads/main-v2\",\"remoteHead\":\"" + remoteHead + "\",\"status\":\"" + status + "\",\"hasLocalPatches\":false,\"message\":\"" + message + "\"}");
    }

    public static int Main(string[] args)
    {
        string mode = Environment.GetEnvironmentVariable("FAKE_SOURCE_UPDATE_MODE") ?? "up-to-date";
        string proxy = string.Join(";", new [] {
            Environment.GetEnvironmentVariable("HTTP_PROXY") ?? "",
            Environment.GetEnvironmentVariable("HTTPS_PROXY") ?? "",
            Environment.GetEnvironmentVariable("ALL_PROXY") ?? ""
        });

        if (mode == "loopback" && proxy.IndexOf("127.0.0.1", StringComparison.OrdinalIgnoreCase) >= 0)
        {
            WriteResult("check-failed", "base-a", "", "proxyconnect tcp: dial tcp 127.0.0.1:7897: connectex: No connection could be made because the target machine actively refused it");
            return 1;
        }
        if (mode == "network")
        {
            WriteResult("check-failed", "base-a", "", "lookup github.com: DNS timeout");
            return 1;
        }
        if (mode == "upstream")
        {
            WriteResult("upstream-update", "base-a", "base-b", "upstream main-v2 changed");
            return 0;
        }

        WriteResult("up-to-date", "base-a", "base-a", "upstream main-v2 matches the local tracking baseline");
        return 0;
    }
}
'@

$fakeCli = Join-Path $runRoot 'fake-source-update.exe'
Add-Type -TypeDefinition $fakeSource -Language CSharp -OutputAssembly $fakeCli -OutputType ConsoleApplication

$savedEnvironment = @{}
foreach ($name in 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'NO_PROXY', 'FAKE_SOURCE_UPDATE_MODE') {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

try {
    $missing = Invoke-AdapterCase -CliPath (Join-Path $runRoot 'missing.exe') -SourceRoot $sourceRoot -ResultPath (Join-Path $runRoot 'missing.json')
    Assert-Equal -Actual $missing.ExitCode -Expected 2 -Label 'missing CLI exit code'

    $env:FAKE_SOURCE_UPDATE_MODE = 'network'
    $networkPath = Join-Path $runRoot 'network.json'
    $network = Invoke-AdapterCase -CliPath $fakeCli -SourceRoot $sourceRoot -ResultPath $networkPath
    Assert-Equal -Actual $network.ExitCode -Expected 12 -Label 'ordinary network failure exit code'
    $networkPayload = Get-Content -LiteralPath $networkPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-Equal -Actual ([string]$networkPayload.status) -Expected 'check-failed' -Label 'ordinary network failure status'
    if ($null -ne $networkPayload.PSObject.Properties['proxyFallback']) {
        throw 'An ordinary network error must not trigger direct fallback'
    }

    $env:FAKE_SOURCE_UPDATE_MODE = 'loopback'
    $env:HTTP_PROXY = 'http://127.0.0.1:7897'
    $env:HTTPS_PROXY = 'http://127.0.0.1:7897'
    $env:ALL_PROXY = 'http://127.0.0.1:7897'
    $loopbackPath = Join-Path $runRoot 'loopback.json'
    $loopback = Invoke-AdapterCase -CliPath $fakeCli -SourceRoot $sourceRoot -ResultPath $loopbackPath
    Assert-Equal -Actual $loopback.ExitCode -Expected 0 -Label 'loopback fallback exit code'
    $loopbackPayload = Get-Content -LiteralPath $loopbackPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-Equal -Actual ([string]$loopbackPayload.status) -Expected 'up-to-date' -Label 'loopback fallback status'
    Assert-Equal -Actual ([string]$loopbackPayload.proxyFallback) -Expected 'direct-after-loopback-refused' -Label 'loopback fallback marker'

    $env:FAKE_SOURCE_UPDATE_MODE = 'upstream'
    $upstreamPath = Join-Path $runRoot 'upstream.json'
    $upstream = Invoke-AdapterCase -CliPath $fakeCli -SourceRoot $sourceRoot -ResultPath $upstreamPath
    Assert-Equal -Actual $upstream.ExitCode -Expected 10 -Label 'upstream update exit code'
    $upstreamPayload = Get-Content -LiteralPath $upstreamPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-Equal -Actual ([string]$upstreamPayload.status) -Expected 'upstream-update' -Label 'upstream update status'
}
finally {
    foreach ($name in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process')
    }
}

$summary = [ordered]@{
    adapter = [IO.Path]::GetFullPath($AdapterPath)
    missingCliExitCode = $missing.ExitCode
    networkFailureExitCode = $network.ExitCode
    networkFailureStatus = [string]$networkPayload.status
    loopbackFallbackExitCode = $loopback.ExitCode
    loopbackFallbackStatus = [string]$loopbackPayload.status
    loopbackFallback = [string]$loopbackPayload.proxyFallback
    upstreamExitCode = $upstream.ExitCode
    upstreamStatus = [string]$upstreamPayload.status
    evidenceRoot = $runRoot
}
$summaryPath = Join-Path $runRoot 'summary.json'
$summary | ConvertTo-Json | Set-Content -LiteralPath $summaryPath -Encoding UTF8
Write-Output "Source-update adapter deterministic verification passed: $summaryPath"
