$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

# Keep Windows packaging independent from Chocolatey's package-index availability.
# The portable archive is pinned and checksum-verified before it reaches PATH.
$version = "3.12"
$url = "https://sourceforge.net/projects/nsis/files/NSIS%203/$version/nsis-$version.zip/download"
$expectedSha256 = "56581f90db321581c5381193d796fffcf2d24b2f8fed2160a6c6a3baa67f2c4f"
$tempRoot = if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$archive = Join-Path $tempRoot "nsis-$version.zip"
$extractRoot = Join-Path $tempRoot "reasonix-nsis"
$nsisDir = Join-Path $extractRoot "nsis-$version"

for ($attempt = 1; $attempt -le 3; $attempt++) {
    try {
        Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
        & curl.exe --fail --location --silent --show-error --connect-timeout 30 --max-time 120 --output $archive $url
        if ($LASTEXITCODE -ne 0) {
            throw "curl failed with exit code $LASTEXITCODE"
        }
        break
    }
    catch {
        if ($attempt -eq 3) {
            throw
        }
        Write-Warning "NSIS download attempt $attempt failed; retrying in $($attempt * 5) seconds"
        Start-Sleep -Seconds ($attempt * 5)
    }
}

$actualSha256 = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualSha256 -ne $expectedSha256) {
    throw "NSIS archive checksum mismatch: expected $expectedSha256, got $actualSha256"
}

Remove-Item -LiteralPath $extractRoot -Recurse -Force -ErrorAction SilentlyContinue
Expand-Archive -LiteralPath $archive -DestinationPath $extractRoot
Remove-Item -LiteralPath $archive -Force

$makensis = Join-Path $nsisDir "makensis.exe"
if (-not (Test-Path -LiteralPath $makensis -PathType Leaf)) {
    throw "NSIS extraction did not produce $makensis"
}
if ([string]::IsNullOrWhiteSpace($env:GITHUB_PATH)) {
    throw "GITHUB_PATH is not set"
}

Add-Content -LiteralPath $env:GITHUB_PATH -Value $nsisDir -Encoding utf8
& $makensis /VERSION
if ($LASTEXITCODE -ne 0) {
    throw "makensis version check failed with exit code $LASTEXITCODE"
}
