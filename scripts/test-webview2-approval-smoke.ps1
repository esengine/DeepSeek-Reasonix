param(
    [Parameter(Mandatory = $true)]
    [string]$ExecutablePath,
    [int]$TimeoutSeconds = 60
)

$ErrorActionPreference = "Stop"
$exe = (Resolve-Path $ExecutablePath).Path
$tempRoot = if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$smokeHome = Join-Path $tempRoot ("reasonix-webview2-approval-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $smokeHome | Out-Null

$oldSmoke = $env:REASONIX_WEBVIEW2_APPROVAL_SMOKE
$oldHome = $env:REASONIX_HOME
try {
    $env:REASONIX_WEBVIEW2_APPROVAL_SMOKE = "1"
    $env:REASONIX_HOME = $smokeHome
    $process = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe) -PassThru
    if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        throw "Wails/WebView2 approval smoke timed out after $TimeoutSeconds seconds"
    }
    if ($process.ExitCode -ne 0) {
        throw "Wails/WebView2 approval smoke exited with code $($process.ExitCode)"
    }
    Write-Host "Wails/WebView2 approval smoke passed"
}
finally {
    $env:REASONIX_WEBVIEW2_APPROVAL_SMOKE = $oldSmoke
    $env:REASONIX_HOME = $oldHome
    if (Test-Path $smokeHome) {
        Remove-Item -LiteralPath $smokeHome -Recurse -Force
    }
}
