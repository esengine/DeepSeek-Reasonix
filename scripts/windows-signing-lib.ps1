# Dot-sourced by every Windows signing and verification script, so each one
# answers "which files are PE images", "which of them does the release sign"
# and "does this file carry that signature" with one judgement each.

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$script:PeExtensions = @(".exe", ".dll", ".node")

# Every PE file the Windows bundle may hold, declared rather than discovered:
# a PE file nobody listed stops the release until a person decides who signs it.
$script:ReleaseSignedPe = @(
    "Reasonix Studio.exe",
    "dxcompiler.dll",
    "ffmpeg.dll",
    "resources/bin/reasonix-computer-helper.exe",
    "resources/bin/reasonix-studio-host.exe",
    "vk_swiftshader.dll",
    "vulkan-1.dll"
)
# Shipped by Electron already signed by Microsoft, and kept that way.
$script:MicrosoftSignedPe = @(
    "d3dcompiler_47.dll",
    "dxil.dll"
)
# Microsoft Root Certificate Authority 2010, the root their signer chains to.
$script:MicrosoftRootThumbprint = "3B1EFD3A66EA28B16697394703A72CA340A05BD5"
# Written by NSIS packaging after the payload is signed, so pinned by content.
# The hash is electron-builder's; an electron-builder upgrade that changes it
# fails the release until it is reviewed and updated here.
$script:InstallerOwnedFiles = @{
    "resources/elevate.exe" = "9b1fbf0c11c520ae714af8aa9af12cfd48503eedecd7398d8992ee94d1b4dc37"
}

function Get-TreeFiles {
    param([Parameter(Mandatory = $true)][string]$Root)

    $rootPath = (Resolve-Path -LiteralPath $Root).Path
    $items = @(Get-ChildItem -LiteralPath $rootPath -Recurse -Force)
    # A link resolves outside the tree the manifest describes, so a hash of the
    # tree would not be a hash of what ships.
    $linked = @($items | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint })
    if ($linked.Count -gt 0) {
        throw "Windows payload contains a link or reparse point: $($linked[0].FullName)"
    }
    foreach ($item in $items) {
        if ($item.PSIsContainer) { continue }
        [pscustomobject]@{
            Path     = [IO.Path]::GetRelativePath($rootPath, $item.FullName).Replace('\', '/')
            FullName = $item.FullName
        }
    }
}

function Test-PeImage {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ($script:PeExtensions -contains [IO.Path]::GetExtension($Path).ToLowerInvariant()) { return $true }
    $stream = [IO.File]::OpenRead($Path)
    try {
        $header = New-Object byte[] 2
        return ($stream.Read($header, 0, 2) -eq 2 -and $header[0] -eq 0x4D -and $header[1] -eq 0x5A)
    }
    finally {
        $stream.Dispose()
    }
}

# Every PE image under Root, by extension or by its MZ header, as sorted
# forward-slash paths relative to Root.
function Get-PeFiles {
    param([Parameter(Mandatory = $true)][string]$Root)

    $paths = [Collections.Generic.List[string]]::new()
    foreach ($file in Get-TreeFiles -Root $Root) {
        if (Test-PeImage -Path $file.FullName) { $paths.Add($file.Path) }
    }
    $paths.Sort([StringComparer]::Ordinal)
    $paths.ToArray()
}

# One "<sha256> <path>" line per file under Root, ordinally sorted by path.
function Get-TreeManifest {
    param([Parameter(Mandatory = $true)][string]$Root)

    $lines = [Collections.Generic.List[string]]::new()
    foreach ($file in Get-TreeFiles -Root $Root) {
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $file.FullName).Hash.ToLowerInvariant()
        $lines.Add("$hash $($file.Path)")
    }
    # A SHA-256 hex digest and its separator are 65 characters, so this orders by path.
    $lines.Sort([Comparison[string]] { param($a, $b) [string]::CompareOrdinal($a.Substring(65), $b.Substring(65)) })
    $lines.ToArray()
}

function Get-ManifestDigest {
    param([Parameter(Mandatory = $true)][AllowEmptyCollection()][string[]]$Manifest)

    $text = ($Manifest | ForEach-Object { "$_`n" }) -join ""
    $bytes = [Text.Encoding]::UTF8.GetBytes($text)
    [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
}

# The PE files under Root must be exactly the declared ones.
function Assert-DeclaredPeSet {
    param([Parameter(Mandatory = $true)][string]$Root)

    $declared = @($script:ReleaseSignedPe + $script:MicrosoftSignedPe)
    $found = @(Get-PeFiles -Root $Root)
    $unexpected = @($found | Where-Object { $declared -cnotcontains $_ })
    $missing = @($declared | Where-Object { $found -cnotcontains $_ })
    foreach ($name in $unexpected) {
        Write-Host "::error title=studio-signing.undeclared-pe::$name is a PE file scripts/windows-signing-lib.ps1 does not declare"
    }
    foreach ($name in $missing) {
        Write-Host "::error title=studio-signing.missing-pe::$name is declared in scripts/windows-signing-lib.ps1 but absent"
    }
    if ($unexpected.Count -gt 0 -or $missing.Count -gt 0) {
        throw "PE files under $Root differ from the declared set"
    }
}

function Get-SignTool {
    $tool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" |
        Sort-Object FullName -Descending | Select-Object -First 1
    if (-not $tool) { throw "Windows SDK SignTool not found" }
    $tool.FullName
}

# The signer of the signature embedded in this file, once SignTool has verified
# it chains to a trusted root and is timestamped. Get-AuthenticodeSignature is
# not used because it answers from the OS catalog first: for d3dcompiler_47.dll
# it reports a catalog signature whatever this copy carries.
function Get-VerifiedSigner {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Signed Windows artifact is missing: $Path" }
    # Without /a SignTool verifies only the embedded signatures; /tw turns a
    # missing timestamp into a non-zero exit.
    $output = & (Get-SignTool) verify /pa /all /tw $Path 2>&1
    if ($LASTEXITCODE -ne 0) {
        $output | ForEach-Object { Write-Host $_ }
        throw "Authenticode signature is missing, untrusted or untimestamped: $Path"
    }
    try {
        [Security.Cryptography.X509Certificates.X509Certificate2]::new(
            [Security.Cryptography.X509Certificates.X509Certificate]::CreateFromSignedFile($Path))
    }
    catch {
        throw "Authenticode signature is missing: $Path"
    }
}

function Get-NameAttribute {
    param(
        [Parameter(Mandatory = $true)][Security.Cryptography.X509Certificates.X500DistinguishedName]$Name,
        [Parameter(Mandatory = $true)][string]$Oid
    )

    @($Name.EnumerateRelativeDistinguishedNames() |
        Where-Object { -not $_.HasMultipleElements -and $_.GetSingleElementType().Value -eq $Oid } |
        ForEach-Object { $_.GetSingleElementValue() })
}

function Assert-EmbeddedSignature {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][ValidatePattern('^[0-9a-fA-F]{40}$')][string]$Thumbprint,
        [string]$Subject
    )

    $certificate = Get-VerifiedSigner -Path $Path
    if ($certificate.Thumbprint -ne $Thumbprint.ToUpperInvariant()) {
        throw "Unexpected signer thumbprint $($certificate.Thumbprint): $Path"
    }
    if ($Subject -and $certificate.Subject -cne $Subject) {
        throw "Unexpected signer subject '$($certificate.Subject)': $Path"
    }
    # The thumbprint pins one certificate and so its issuer; a verified chain
    # already means that issuer chains to a trusted root.
    if ([string]::IsNullOrWhiteSpace($certificate.Issuer)) { throw "Signer certificate has no issuer: $Path" }
    Write-Host "Authenticode verified: $Path"
}

function Assert-MicrosoftSignature {
    param([Parameter(Mandatory = $true)][string]$Path)

    $certificate = Get-VerifiedSigner -Path $Path
    $organization = "2.5.4.10"
    $commonName = "2.5.4.3"
    if ((Get-NameAttribute -Name $certificate.SubjectName -Oid $organization) -cnotcontains "Microsoft Corporation") {
        throw "Expected a Microsoft Corporation signer, got '$($certificate.Subject)': $Path"
    }
    $issuerNames = @(Get-NameAttribute -Name $certificate.IssuerName -Oid $commonName)
    if ((Get-NameAttribute -Name $certificate.IssuerName -Oid $organization) -cnotcontains "Microsoft Corporation" -or
        @($issuerNames | Where-Object { $_ -cmatch '^Microsoft .*PCA( \d{4})?$' }).Count -ne 1) {
        throw "Expected a Microsoft PCA issuer, got '$($certificate.Issuer)': $Path"
    }
    # Names can be issued to anyone a trusted CA vouches for; the root cannot.
    $chain = [Security.Cryptography.X509Certificates.X509Chain]::new()
    try {
        $chain.ChainPolicy.RevocationMode = [Security.Cryptography.X509Certificates.X509RevocationMode]::NoCheck
        # SignTool has already checked validity at the timestamped signing time.
        $chain.ChainPolicy.VerificationFlags = [Security.Cryptography.X509Certificates.X509VerificationFlags]::IgnoreNotTimeValid
        $built = $chain.Build($certificate)
        $elements = @($chain.ChainElements)
        $root = if ($elements.Count -gt 0) { $elements[-1].Certificate.Thumbprint } else { "" }
        if (-not $built -or $root -ne $script:MicrosoftRootThumbprint) {
            throw "Microsoft signer does not chain to Microsoft Root Certificate Authority 2010 (root $root): $Path"
        }
    }
    finally {
        $chain.Dispose()
    }
    Write-Host "Authenticode verified (Microsoft): $Path"
}
