[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern("^[0-9a-fA-F-]{36}$")]
    [string]$OrganizationId,

    [Parameter(Mandatory = $true)]
    [ValidatePattern("^[0-9a-fA-F-]{36}$")]
    [string]$SigningRequestId,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$ExpectedSigningPolicySlug,

    [Parameter(Mandatory = $true)]
    [ValidateNotNullOrEmpty()]
    [string]$OutputArtifactDirectory,

    [ValidateNotNullOrEmpty()]
    [string]$ApiUrl = "https://app.signpath.io/api",

    [ValidateRange(30, 7200)]
    [int]$TimeoutSeconds = 1800,

    [ValidateRange(1, 60)]
    [int]$PollIntervalSeconds = 5,

    [switch]$WaitForExternalApproval
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$apiToken = $env:SIGNPATH_API_TOKEN
if ([string]::IsNullOrWhiteSpace($apiToken)) {
    throw "SIGNPATH_API_TOKEN is required to approve and download a SignPath request."
}

$parsedApiUrl = $null
if (
    -not [Uri]::TryCreate($ApiUrl, [UriKind]::Absolute, [ref]$parsedApiUrl) -or
    $parsedApiUrl.Scheme -ne "https"
) {
    throw "ApiUrl must be an absolute HTTPS URL."
}

$workspace = if ([string]::IsNullOrWhiteSpace($env:GITHUB_WORKSPACE)) {
    [IO.Path]::GetFullPath((Get-Location).Path)
} else {
    [IO.Path]::GetFullPath($env:GITHUB_WORKSPACE)
}
$outputDirectory = if ([IO.Path]::IsPathRooted($OutputArtifactDirectory)) {
    [IO.Path]::GetFullPath($OutputArtifactDirectory)
} else {
    [IO.Path]::GetFullPath((Join-Path $workspace $OutputArtifactDirectory))
}
$workspacePrefix = $workspace.TrimEnd(
    [IO.Path]::DirectorySeparatorChar,
    [IO.Path]::AltDirectorySeparatorChar
) + [IO.Path]::DirectorySeparatorChar
if (-not $outputDirectory.StartsWith($workspacePrefix, [StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputArtifactDirectory must resolve inside GITHUB_WORKSPACE."
}

$headers = @{
    Authorization = "Bearer $apiToken"
}
$requestBaseUrl = (
    $ApiUrl.TrimEnd("/") +
    "/v1/$OrganizationId/SigningRequests/$SigningRequestId"
)

$request = Invoke-RestMethod `
    -Method Get `
    -Uri $requestBaseUrl `
    -Headers $headers
if ($request.signingPolicySlug -ne $ExpectedSigningPolicySlug) {
    throw (
        "SignPath request policy mismatch: got '$($request.signingPolicySlug)', " +
        "expected '$ExpectedSigningPolicySlug'."
    )
}

$deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
$approvalSubmitted = $false
$externalApprovalNoticeWritten = $false
while ($true) {
    $status = Invoke-RestMethod `
        -Method Get `
        -Uri "$requestBaseUrl/Status" `
        -Headers $headers
    Write-Host (
        "SignPath request $SigningRequestId status: " +
        "$($status.status) ($($status.workflowStatus))"
    )

    if ($status.isFinalStatus) {
        if ($status.status -ne "Completed") {
            throw (
                "SignPath request ended with status '$($status.status)' " +
                "and workflow status '$($status.workflowStatus)'."
            )
        }
        break
    }

    if ($status.status -eq "WaitingForApproval" -and -not $approvalSubmitted) {
        if ($WaitForExternalApproval) {
            if (-not $externalApprovalNoticeWritten) {
                Write-Host (
                    "Waiting for an authorized SignPath user to approve request " +
                    "$SigningRequestId."
                )
                $externalApprovalNoticeWritten = $true
            }
        } else {
            Invoke-RestMethod `
                -Method Post `
                -Uri "$requestBaseUrl/Approve" `
                -Headers $headers | Out-Null
            $approvalSubmitted = $true
            Write-Host "Approved SignPath request $SigningRequestId through the release CI identity."
        }
    }

    if ([DateTimeOffset]::UtcNow -ge $deadline) {
        throw "Timed out waiting for SignPath request $SigningRequestId."
    }
    Start-Sleep -Seconds $PollIntervalSeconds
}

$temporaryRoot = if ([string]::IsNullOrWhiteSpace($env:RUNNER_TEMP)) {
    [IO.Path]::GetTempPath()
} else {
    $env:RUNNER_TEMP
}
$temporaryArchive = Join-Path $temporaryRoot "signpath-$SigningRequestId.zip"
try {
    Invoke-WebRequest `
        -Method Get `
        -Uri "$requestBaseUrl/SignedArtifact" `
        -Headers $headers `
        -OutFile $temporaryArchive
    if (-not (Test-Path -LiteralPath $temporaryArchive -PathType Leaf)) {
        throw "SignPath did not return a signed artifact."
    }
    if ((Get-Item -LiteralPath $temporaryArchive).Length -eq 0) {
        throw "SignPath returned an empty signed artifact."
    }

    New-Item -ItemType Directory -Force -Path $outputDirectory | Out-Null
    Expand-Archive `
        -LiteralPath $temporaryArchive `
        -DestinationPath $outputDirectory `
        -Force
} finally {
    Remove-Item -LiteralPath $temporaryArchive -Force -ErrorAction SilentlyContinue
}

Write-Host "Downloaded and extracted signed artifact to $outputDirectory."
