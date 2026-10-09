# Reads the Authenticode signatures back off the Windows files that will ship.
# A signature the release job believes it applied but that is not on the bytes
# is the one failure the signing requests cannot catch about themselves, so
# every file is checked from disk rather than trusted from the step order.
# Given a payload it checks the whole tree; given a file, that file.
[CmdletBinding(DefaultParameterSetName = "Payload")]
param(
    [Parameter(Mandatory = $true, ParameterSetName = "Payload")]
    [string]$PayloadDirectory,

    [Parameter(ParameterSetName = "Payload")]
    [string]$DigestPath,

    [Parameter(Mandatory = $true, ParameterSetName = "Payload")]
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture,

    [Parameter(Mandatory = $true, ParameterSetName = "File")]
    [string]$FilePath,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[0-9a-fA-F]{40}$')]
    [string]$ExpectedThumbprint,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$ExpectedSubject
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "windows-signing-lib.ps1")

if ($PSCmdlet.ParameterSetName -eq "File") {
    Assert-EmbeddedSignature -Path $FilePath -Thumbprint $ExpectedThumbprint -Subject $ExpectedSubject
    exit 0
}

$root = (Resolve-Path -LiteralPath $PayloadDirectory).Path
Assert-DeclaredPeSet -Root $root
Assert-PeArchitecture -Root $root -Architecture $Architecture
foreach ($name in $script:ReleaseSignedPe) {
    Assert-EmbeddedSignature -Path (Join-Path $root $name) -Thumbprint $ExpectedThumbprint -Subject $ExpectedSubject
}
foreach ($name in $script:MicrosoftSignedPe) {
    Assert-MicrosoftSignature -Path (Join-Path $root $name)
}
$digest = Get-ManifestDigest -Manifest @(Get-TreeManifest -Root $root)
if ($DigestPath) { Set-Content -LiteralPath $DigestPath -Value $digest -NoNewline }
Write-Host "Windows Authenticode payload verified; tree digest $digest."
