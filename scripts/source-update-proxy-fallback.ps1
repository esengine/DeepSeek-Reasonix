Set-StrictMode -Version Latest

function Test-SourceUpdateLoopbackProxyRefusal {
    param(
        [Parameter(Mandatory = $true)][string]$Status,
        [Parameter(Mandatory = $true)][string]$Message
    )

    if ($Status -ne 'check-failed') {
        return $false
    }
    $mentionsProxy = $Message -match '(?i)(proxyconnect|over proxy|proxy)'
    $mentionsLoopback = $Message -match '(?i)(127\.0\.0\.1|localhost|\[::1\])'
    $mentionsRefusal = $Message -match '(?i)(refused|could not connect|no connection could be made)'
    return $mentionsProxy -and $mentionsLoopback -and $mentionsRefusal
}

function Set-SourceUpdateDirectEnvironment {
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.ProcessStartInfo]$StartInfo
    )

    foreach ($name in 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy') {
        $StartInfo.EnvironmentVariables.Remove($name)
    }

    $noProxy = @($StartInfo.EnvironmentVariables['NO_PROXY'] -split ',') |
        ForEach-Object { $_.Trim() } |
        Where-Object { $_ }
    foreach ($hostName in 'github.com', 'api.github.com') {
        if ($noProxy -notcontains $hostName) {
            $noProxy += $hostName
        }
    }
    $StartInfo.EnvironmentVariables['NO_PROXY'] = $noProxy -join ','
}
