[CmdletBinding()]
param(
    [int]$Port = 4173,
    [int]$Iterations = 3,
    [string]$ArtifactsDirectory = "",
    [string]$PreviewFrontendRoot = "",
    [string]$Commit = "",
    [int]$Width = 1200,
    [int]$Height = 800
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$desktopRoot = Join-Path $repoRoot "desktop"
$frontendRoot = Join-Path $desktopRoot "frontend"
$previewRoot = if ($PreviewFrontendRoot) {
    [IO.Path]::GetFullPath($PreviewFrontendRoot)
}
else {
    $frontendRoot
}
$artifactRoot = if ($ArtifactsDirectory) {
    [IO.Path]::GetFullPath($ArtifactsDirectory)
}
elseif ($env:RUNNER_TEMP) {
    Join-Path $env:RUNNER_TEMP "reasonix-transcript-selection-smoke"
}
else {
    Join-Path ([IO.Path]::GetTempPath()) "reasonix-transcript-selection-smoke"
}
New-Item -ItemType Directory -Path $artifactRoot -Force | Out-Null

$previewOut = Join-Path $artifactRoot "preview.stdout.log"
$previewErr = Join-Path $artifactRoot "preview.stderr.log"
$nativeLog = Join-Path $artifactRoot "native.log"
$resultPath = Join-Path $artifactRoot "result.json"
$testedCommit = if ($Commit) { $Commit } elseif ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { "local" }
$url = "http://127.0.0.1:$Port/?mock=bench&bench=1"
$pnpm = (Get-Command pnpm.cmd -ErrorAction SilentlyContinue)
if ($null -eq $pnpm) {
    $pnpm = Get-Command pnpm
}

$preview = Start-Process `
    -FilePath $pnpm.Source `
    -ArgumentList @("exec", "vite", "preview", "--port", "$Port", "--strictPort", "--host", "127.0.0.1") `
    -WorkingDirectory $previewRoot `
    -RedirectStandardOutput $previewOut `
    -RedirectStandardError $previewErr `
    -PassThru

try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    $ready = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        if ($preview.HasExited) {
            throw "frontend preview exited before becoming ready"
        }
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 2
            if ($response.StatusCode -lt 500) {
                $ready = $true
                break
            }
        }
        catch {
            Start-Sleep -Milliseconds 150
        }
    }
    if (-not $ready) {
        throw "frontend preview did not become ready at $url"
    }

    Push-Location $desktopRoot
    try {
        $nativeErrorPreference = $ErrorActionPreference
        $ErrorActionPreference = "Continue"
        & go run -tags reasonix_transcript_smoke ./cmd/transcript-selection-smoke `
            -url $url `
            -script "transcript_selection_smoke_contract.js" `
            -artifacts $artifactRoot `
            -result-file $resultPath `
            -iterations $Iterations `
            -width $Width `
            -height $Height `
            -commit $testedCommit 2>&1 | Tee-Object -FilePath $nativeLog
        $nativeExitCode = $LASTEXITCODE
        $ErrorActionPreference = $nativeErrorPreference
        if ($nativeExitCode -ne 0) {
            throw "native transcript selection smoke failed with exit code $nativeExitCode"
        }
    }
    finally {
        Pop-Location
    }
}
finally {
    if (-not $preview.HasExited) {
        & taskkill.exe /PID $preview.Id /T /F 2>$null | Out-Null
    }
}

Write-Host "WebView2 transcript selection smoke passed; artifacts: $artifactRoot"
