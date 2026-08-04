param(
    [Parameter(Mandatory = $true)]
    [string]$PayloadDirectory,

    [Parameter(Mandatory = $true)]
    [string]$InstallerPath,

    [Parameter(Mandatory = $true)]
    [string]$PortableArchivePath,

    [switch]$RequireTrusted
)

$ErrorActionPreference = "Stop"

$expectedPayload = @(
    "reasonix-desktop.exe",
    "reasonix-guard.exe",
    "reasonix-launcher.exe",
    "reasonix-update-helper.exe",
    "reasonix-cli.exe",
    "reasonix-uninstall.exe"
)

function Assert-AuthenticodeSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Signed Windows artifact is missing: $Path"
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($null -eq $signature.SignerCertificate -or $signature.SignatureType -eq "None") {
        throw "Authenticode signature is missing: $Path"
    }
    if ($RequireTrusted -and $signature.Status -ne "Valid") {
        throw "Authenticode signature is not trusted for $Path`: $($signature.Status) $($signature.StatusMessage)"
    }
    Write-Host "Authenticode $($signature.Status): $Path"
}

$payloadFiles = @(Get-ChildItem -LiteralPath $PayloadDirectory -File -Filter "*.exe")
if ($payloadFiles.Count -ne $expectedPayload.Count) {
    throw "Payload must contain exactly $($expectedPayload.Count) executables, found $($payloadFiles.Count)"
}
foreach ($name in $expectedPayload) {
    Assert-AuthenticodeSignature -Path (Join-Path $PayloadDirectory $name)
}
Assert-AuthenticodeSignature -Path $InstallerPath

$extractRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("reasonix-authenticode-" + [guid]::NewGuid().ToString("N"))
try {
    Expand-Archive -LiteralPath $PortableArchivePath -DestinationPath $extractRoot

    # Legacy portable releases kept all six executables at InstallRoot. The
    # versioned-v1 layout deliberately keeps only the launcher aliases and CLI
    # at the root, while the active Desktop, update helper, and CLI live under
    # versions/vX.Y.Z/. Verify the exact layout selected by current.json instead
    # of treating the three versioned executables as missing.
    $currentPath = Join-Path $extractRoot "current.json"
    if (Test-Path -LiteralPath $currentPath -PathType Leaf) {
        $current = Get-Content -LiteralPath $currentPath -Raw | ConvertFrom-Json
        if ($current.schemaVersion -ne 1) {
            throw "Portable current.json schemaVersion must be 1"
        }
        $activeVersion = [string]$current.activeVersion
        $activeDir = [string]$current.activeDir
        if ($activeVersion -notmatch '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$' -or
            [string]::IsNullOrWhiteSpace($activeDir) -or
            $activeDir.Replace("\", "/") -ne "versions/$activeVersion") {
            throw "Portable current.json must bind activeVersion to versions/<activeVersion>"
        }

        $activePath = [System.IO.Path]::GetFullPath((Join-Path $extractRoot $activeDir))
        $extractPrefix = [System.IO.Path]::GetFullPath($extractRoot).TrimEnd([char[]]@('\', '/')) + [System.IO.Path]::DirectorySeparatorChar
        if (-not $activePath.StartsWith($extractPrefix, [System.StringComparison]::OrdinalIgnoreCase) -or
            -not (Test-Path -LiteralPath $activePath -PathType Container)) {
            throw "Portable current.json activeDir escapes or is missing: $activeDir"
        }

        $portableSources = @(
            [pscustomobject]@{ Portable = "reasonix-launcher.exe"; Payload = "reasonix-launcher.exe" },
            [pscustomobject]@{ Portable = "Reasonix.exe"; Payload = "reasonix-launcher.exe" },
            [pscustomobject]@{ Portable = "reasonix-cli.exe"; Payload = "reasonix-cli.exe" },
            [pscustomobject]@{ Portable = (Join-Path $activeDir "reasonix-desktop.exe"); Payload = "reasonix-desktop.exe" },
            [pscustomobject]@{ Portable = (Join-Path $activeDir "reasonix-update-helper.exe"); Payload = "reasonix-update-helper.exe" },
            [pscustomobject]@{ Portable = (Join-Path $activeDir "reasonix-cli.exe"); Payload = "reasonix-cli.exe" }
        )
    }
    else {
        $portableSources = @(
            [pscustomobject]@{ Portable = "reasonix-desktop.exe"; Payload = "reasonix-desktop.exe" },
            [pscustomobject]@{ Portable = "reasonix-guard.exe"; Payload = "reasonix-guard.exe" },
            [pscustomobject]@{ Portable = "reasonix-launcher.exe"; Payload = "reasonix-launcher.exe" },
            [pscustomobject]@{ Portable = "Reasonix.exe"; Payload = "reasonix-launcher.exe" },
            [pscustomobject]@{ Portable = "reasonix-update-helper.exe"; Payload = "reasonix-update-helper.exe" },
            [pscustomobject]@{ Portable = "reasonix-cli.exe"; Payload = "reasonix-cli.exe" }
        )
    }

    $portableFiles = @(Get-ChildItem -LiteralPath $extractRoot -Recurse -File -Filter "*.exe")
    if ($portableFiles.Count -ne 6) {
        throw "Portable archive must contain exactly 6 executables, found $($portableFiles.Count)"
    }

    foreach ($entry in $portableSources) {
        $portablePath = Join-Path $extractRoot $entry.Portable
        Assert-AuthenticodeSignature -Path $portablePath
        $portableHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $portablePath).Hash
        $payloadHash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $PayloadDirectory $entry.Payload)).Hash
        if ($portableHash -ne $payloadHash) {
            throw "Portable $($entry.Portable) does not match signed payload $($entry.Payload)"
        }
    }
}
finally {
    if (Test-Path -LiteralPath $extractRoot) {
        Remove-Item -LiteralPath $extractRoot -Recurse -Force
    }
}

Write-Host "Windows Authenticode release contract verified."
