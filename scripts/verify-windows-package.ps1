# Checks that the installer and the portable archive windows-package built hold
# exactly the signed payload. It unpacks both, so it runs in a job that holds
# no secrets; the signing job then accepts only the file hashes written here.
param(
    [Parameter(Mandatory = $true)]
    [string]$PayloadDirectory,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-f]{64}$')]
    [string]$ExpectedPayloadDigest,

    [Parameter(Mandatory = $true)]
    [string]$InstallerPath,

    [Parameter(Mandatory = $true)]
    [string]$PortableArchivePath,

    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "windows-signing-lib.ps1")

# A file added, dropped or changed after signing is refused whether or not it
# is a PE image, since app.asar runs as surely as an executable does.
function Assert-TreeMatchesPayload {
    param(
        [Parameter(Mandatory = $true)][string]$Label,
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string[]]$PayloadManifest,
        [hashtable]$Extra = @{}
    )

    $expected = @{}
    foreach ($line in $PayloadManifest) { $expected[$line.Substring(65)] = $line.Substring(0, 64) }
    foreach ($entry in $Extra.GetEnumerator()) { $expected[$entry.Key] = $entry.Value }
    $problems = [Collections.Generic.List[string]]::new()
    $seen = @{}
    foreach ($line in Get-TreeManifest -Root $Root) {
        $path = $line.Substring(65)
        $seen[$path] = $true
        if (-not $expected.ContainsKey($path)) { $problems.Add("not in the signed payload: $path") }
        elseif ($expected[$path] -ne $line.Substring(0, 64)) { $problems.Add("differs from the signed payload: $path") }
    }
    foreach ($path in $expected.Keys) {
        if (-not $seen.ContainsKey($path)) { $problems.Add("missing from the package: $path") }
    }
    if ($problems.Count -gt 0) {
        foreach ($problem in $problems) { Write-Host "::error title=studio-signing.tree-mismatch::$Label $problem" }
        throw "$Label does not hold the signed payload ($($problems.Count) differences)"
    }
    Write-Host "$Label matches the signed payload ($($expected.Count) files)."
}

function Invoke-SevenZip {
    param([Parameter(Mandatory = $true)][string]$Archive, [Parameter(Mandatory = $true)][string]$Destination)

    $sevenZip = Join-Path $env:ProgramFiles "7-Zip\7z.exe"
    if (-not (Test-Path -LiteralPath $sevenZip -PathType Leaf)) { throw "7-Zip is required to read the installer: $sevenZip" }
    & $sevenZip x -y "-o$Destination" $Archive | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "7-Zip could not read $Archive`: exit $LASTEXITCODE" }
}

# The digest was recorded by the job that signed the payload, as a job output
# no later job can write. A payload replaced in between stops here.
$payloadRoot = (Resolve-Path -LiteralPath $PayloadDirectory).Path
$payloadManifest = @(Get-TreeManifest -Root $payloadRoot)
$payloadDigest = Get-ManifestDigest -Manifest $payloadManifest
if ($payloadDigest -ne $ExpectedPayloadDigest) {
    throw "Signed payload digest $payloadDigest does not match the $ExpectedPayloadDigest the signing job recorded"
}
Assert-DeclaredPeSet -Root $payloadRoot

# Hashed before either is opened, so the hashes name the bytes that were checked.
$installerHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $InstallerPath).Hash.ToLowerInvariant()
$archiveHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $PortableArchivePath).Hash.ToLowerInvariant()

$extractRoot = Join-Path ([IO.Path]::GetTempPath()) ("reasonix-package-" + [guid]::NewGuid().ToString("N"))
try {
    $portableRoot = Join-Path $extractRoot "portable"
    Expand-Archive -LiteralPath $PortableArchivePath -DestinationPath $portableRoot
    Assert-TreeMatchesPayload -Label "Portable archive" -Root $portableRoot -PayloadManifest $payloadManifest

    # electron-builder's NSIS installer carries each architecture's application
    # as an app-<arch>.7z among its plugins; this release builds x64 alone.
    $installerRoot = Join-Path $extractRoot "installer"
    Invoke-SevenZip -Archive $InstallerPath -Destination $installerRoot
    $appArchives = @(Get-ChildItem -LiteralPath (Join-Path $installerRoot '$PLUGINSDIR') -Filter 'app-*.7z' -File -ErrorAction SilentlyContinue |
        ForEach-Object Name)
    if ($appArchives.Count -ne 1 -or $appArchives[0] -cne 'app-64.7z') {
        throw "Installer must carry exactly one application archive, app-64.7z; found: $($appArchives -join ', ')"
    }
    $installedRoot = Join-Path $extractRoot "installed"
    Invoke-SevenZip -Archive (Join-Path $installerRoot '$PLUGINSDIR\app-64.7z') -Destination $installedRoot
    Assert-TreeMatchesPayload -Label "Installer" -Root $installedRoot -PayloadManifest $payloadManifest -Extra $script:InstallerOwnedFiles
}
finally {
    if (Test-Path -LiteralPath $extractRoot) {
        Remove-Item -LiteralPath $extractRoot -Recurse -Force
    }
}

"installer-sha256=$installerHash" | Add-Content -LiteralPath $OutputPath
"archive-sha256=$archiveHash" | Add-Content -LiteralPath $OutputPath
Write-Host "Windows packages match the signed payload: installer $installerHash, archive $archiveHash."
