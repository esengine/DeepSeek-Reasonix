param(
    [string]$OutputDirectory = ".codex-build/windows-sandbox-native"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$output = Join-Path $repoRoot $OutputDirectory
New-Item -ItemType Directory -Force -Path $output | Out-Null

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$metadataPath = Join-Path $output "$stamp-metadata.txt"
$testPath = Join-Path $output "$stamp-tests.log"
$benchmarkPath = Join-Path $output "$stamp-benchmark.log"

@(
    "timestamp=$([DateTimeOffset]::Now.ToString('o'))"
    "computer=$env:COMPUTERNAME"
    "os=$([Environment]::OSVersion.VersionString)"
    "architecture=$env:PROCESSOR_ARCHITECTURE"
    "go=$(go version)"
    "commit=$(git -C $repoRoot rev-parse HEAD)"
) | Set-Content -Encoding utf8 $metadataPath

Push-Location $repoRoot
try {
    go test -count=1 -timeout=8m ./internal/winsandbox ./internal/sandbox 2>&1 |
        Tee-Object -FilePath $testPath
    if ($LASTEXITCODE -ne 0) { throw "native Windows sandbox tests failed" }

    go test -run '^$' -bench '^BenchmarkWindowsRestrictedWorkspaceWrite$' `
        -benchtime=100x -count=1 ./internal/winsandbox 2>&1 |
        Tee-Object -FilePath $benchmarkPath
    if ($LASTEXITCODE -ne 0) { throw "native Windows sandbox benchmark failed" }
}
finally {
    Pop-Location
}

Write-Host "Windows sandbox evidence written to $output"
