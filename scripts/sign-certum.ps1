param(
    [Parameter(Mandatory = $true, ParameterSetName = 'Payload')]
    [string]$PayloadDirectory,
    [Parameter(Mandatory = $true, ParameterSetName = 'File')]
    [string]$FilePath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$thumbprint = $env:CERTUM_KEY_ID
if ($thumbprint -notmatch '^[0-9a-fA-F]{40}$') { throw 'Invalid CERTUM_KEY_ID' }
. (Join-Path $PSScriptRoot 'windows-signing-lib.ps1')
$tool = Get-SignTool

if ($PSCmdlet.ParameterSetName -eq 'Payload') {
    $root = (Resolve-Path -LiteralPath $PayloadDirectory).Path
    $prefix = $root.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    $entries = @(Get-Content -LiteralPath (Join-Path $root 'signing-files.txt') |
        ForEach-Object { $_.Trim() } | Where-Object { $_ -and -not $_.StartsWith('#') })
    if ($entries.Count -eq 0) { throw 'Empty signing manifest' }
    $files = @($entries | ForEach-Object {
        $candidate = [IO.Path]::GetFullPath((Join-Path $root $_))
        if (-not $candidate.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Signing manifest path escapes payload directory'
        }
        $candidate
    })
} else {
    $files = @((Resolve-Path -LiteralPath $FilePath).Path)
}

# Validate the entire manifest before the first signing operation.
foreach ($file in $files) {
    if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Missing signing input: $file" }
}
foreach ($file in $files) {
    & $tool sign /sha1 $thumbprint /fd SHA256 /tr http://timestamp.certum.pl /td SHA256 $file
    if ($LASTEXITCODE -ne 0) { throw "Authenticode signing failed: $file" }
    Assert-EmbeddedSignature -Path $file -Thumbprint $thumbprint
}
