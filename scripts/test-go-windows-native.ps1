[CmdletBinding()]
param(
    [switch]$PreflightOnly,
    [string]$GoExecutable,
    [string]$ResultsDirectory,
    [switch]$FirewallBroker,
    [switch]$CleanupFirewallLease,
    [string]$BrokerControlDirectory,
    [int]$ParentProcessId,
    [string]$RunToken
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$requiredGoVersion = "go1.26.5"
$firewallGroup = "intelifar native Go tests"

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Write-Utf8File {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string[]]$Content
    )

    $utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, $Content, $utf8WithoutBom)
}

function Write-BrokerJson {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][hashtable]$Value
    )

    Write-Utf8File -Path $Path -Content @(($Value | ConvertTo-Json -Depth 4))
}

function Invoke-FirewallBroker {
    if (-not (Test-IsAdministrator)) {
        throw "The firewall lease broker must be approved through Windows UAC."
    }
    if ($RunToken -notmatch '^[0-9a-f]{32}$') {
        throw "Invalid firewall lease token."
    }
    if ($ParentProcessId -le 0) {
        throw "A parent process ID is required for the firewall lease."
    }
    if (-not (Test-Path -LiteralPath $BrokerControlDirectory -PathType Container)) {
        throw "Firewall broker control directory does not exist: $BrokerControlDirectory"
    }

    $controlDirectory = [System.IO.Path]::GetFullPath($BrokerControlDirectory)
    $readyPath = Join-Path $controlDirectory "ready.json"
    $stopPath = Join-Path $controlDirectory "stop"
    $donePath = Join-Path $controlDirectory "done.json"
    $failedPath = Join-Path $controlDirectory "failed.json"
    $ruleNames = @(
        "intelifar-go-test-$RunToken-ipv4",
        "intelifar-go-test-$RunToken-ipv6"
    )
    $brokerError = ""
    $cleanupErrors = New-Object System.Collections.Generic.List[string]
    $createdRules = New-Object System.Collections.Generic.List[string]

    try {
        $ipv6Loopback = Get-NetIPInterface -AddressFamily IPv6 -ErrorAction Stop |
            Where-Object { $_.InterfaceIndex -eq 1 } |
            Select-Object -First 1
        if ($null -eq $ipv6Loopback) {
            throw "Windows IPv6 loopback interface was not found."
        }
        $ruleSpecs = @(
            [pscustomobject]@{ Name = $ruleNames[0]; Address = "127.0.0.1"; InterfaceAlias = ""; Label = "IPv4" },
            [pscustomobject]@{ Name = $ruleNames[1]; Address = ""; InterfaceAlias = $ipv6Loopback.InterfaceAlias; Label = "IPv6" }
        )
        foreach ($spec in $ruleSpecs) {
            $ruleParameters = @{
                Name = $spec.Name
                DisplayName = "intelifar Go test loopback $($spec.Label) ($RunToken)"
                Group = $firewallGroup
                Direction = "Inbound"
                Action = "Allow"
                Enabled = "True"
                Profile = "Any"
                Protocol = "TCP"
                EdgeTraversalPolicy = "Block"
            }
            if (-not [string]::IsNullOrWhiteSpace($spec.Address)) {
                $ruleParameters.LocalAddress = $spec.Address
                $ruleParameters.RemoteAddress = $spec.Address
            }
            else {
                # Windows rejects ::1 as an explicit firewall address. Scoping
                # both directions to interface index 1 keeps this rule on the
                # IPv6 loopback adapter without widening it to a LAN adapter.
                $ruleParameters.InterfaceAlias = $spec.InterfaceAlias
            }
            New-NetFirewallRule @ruleParameters | Out-Null
            $createdRules.Add($spec.Name)
        }

        Write-BrokerJson -Path $readyPath -Value @{
            ready = $true
            ruleNames = $ruleNames
            parentProcessId = $ParentProcessId
            createdAt = (Get-Date).ToString("o")
        }

        $leaseDeadline = (Get-Date).AddMinutes(45)
        while (-not (Test-Path -LiteralPath $stopPath -PathType Leaf)) {
            if ((Get-Date) -ge $leaseDeadline) {
                throw "Firewall lease reached its 45 minute safety limit."
            }
            if ($null -eq (Get-Process -Id $ParentProcessId -ErrorAction SilentlyContinue)) {
                break
            }
            Start-Sleep -Milliseconds 250
        }
    }
    catch {
        $brokerError = $_.Exception.Message
        Write-BrokerJson -Path $failedPath -Value @{
            ready = $false
            error = $brokerError
            failedAt = (Get-Date).ToString("o")
        }
    }
    finally {
        foreach ($ruleName in $createdRules) {
            try {
                Remove-NetFirewallRule -Name $ruleName -ErrorAction Stop
            }
            catch {
                $cleanupErrors.Add("$ruleName`: $($_.Exception.Message)")
            }
        }
        foreach ($ruleName in $ruleNames) {
            if ($null -ne (Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue)) {
                $cleanupErrors.Add("$ruleName still exists after cleanup")
            }
        }

        Write-BrokerJson -Path $donePath -Value @{
            success = ([string]::IsNullOrWhiteSpace($brokerError) -and $cleanupErrors.Count -eq 0)
            brokerError = $brokerError
            cleanupErrors = @($cleanupErrors)
            finishedAt = (Get-Date).ToString("o")
        }
    }

    if (-not [string]::IsNullOrWhiteSpace($brokerError) -or $cleanupErrors.Count -gt 0) {
        exit 1
    }
    exit 0
}

function Remove-FirewallLease {
    if (-not (Test-IsAdministrator)) {
        throw "Firewall lease cleanup must be approved through Windows UAC."
    }
    if ($RunToken -notmatch '^[0-9a-f]{32}$') {
        throw "Invalid firewall lease token."
    }

    $ruleNames = @(
        "intelifar-go-test-$RunToken-ipv4",
        "intelifar-go-test-$RunToken-ipv6"
    )
    foreach ($ruleName in $ruleNames) {
        if ($null -ne (Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue)) {
            Remove-NetFirewallRule -Name $ruleName -ErrorAction Stop
        }
    }
    foreach ($ruleName in $ruleNames) {
        if ($null -ne (Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue)) {
            throw "Firewall rule still exists after cleanup: $ruleName"
        }
    }
    Write-Host "Firewall lease $RunToken removed."
    exit 0
}

if ($FirewallBroker) {
    Invoke-FirewallBroker
}
if ($CleanupFirewallLease) {
    Remove-FirewallLease
}

function Resolve-PinnedGo {
    param([string]$RequestedPath)

    $candidates = New-Object System.Collections.Generic.List[string]
    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        $candidates.Add($RequestedPath)
    }
    if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $candidates.Add((Join-Path $env:LOCALAPPDATA "Codex\toolchains\go1.26.5.windows-amd64\go\bin\go.exe"))
    }
    $command = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) {
        $candidates.Add($command.Source)
    }

    foreach ($candidate in $candidates) {
        if ([string]::IsNullOrWhiteSpace($candidate) -or -not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            continue
        }
        $resolved = (Resolve-Path -LiteralPath $candidate).Path
        $versionOutput = (& $resolved version 2>&1 | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $versionOutput -match "\b$([regex]::Escape($requiredGoVersion))\b") {
            return [pscustomobject]@{ Path = $resolved; Version = $versionOutput }
        }
    }

    throw "Go $requiredGoVersion was not found. Pass -GoExecutable or install the verified portable toolchain."
}

function Resolve-GitBash {
    $candidates = New-Object System.Collections.Generic.List[string]
    if (-not [string]::IsNullOrWhiteSpace($env:ProgramFiles)) {
        $candidates.Add((Join-Path $env:ProgramFiles "Git\bin\bash.exe"))
    }
    $programFilesX86 = [Environment]::GetEnvironmentVariable("ProgramFiles(x86)")
    if (-not [string]::IsNullOrWhiteSpace($programFilesX86)) {
        $candidates.Add((Join-Path $programFilesX86 "Git\bin\bash.exe"))
    }
    if (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $candidates.Add((Join-Path $env:LOCALAPPDATA "Programs\Git\bin\bash.exe"))
    }
    $gitCommand = Get-Command git.exe -ErrorAction SilentlyContinue
    if ($null -ne $gitCommand) {
        $gitRoot = Split-Path -Parent (Split-Path -Parent $gitCommand.Source)
        $candidates.Add((Join-Path $gitRoot "bin\bash.exe"))
    }

    foreach ($candidate in $candidates) {
        if ([string]::IsNullOrWhiteSpace($candidate) -or -not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            continue
        }
        $resolved = (Resolve-Path -LiteralPath $candidate).Path
        if ($resolved -match '(?i)\\windows\\system32\\bash\.exe$') {
            continue
        }
        $versionOutput = (& $resolved --version 2>&1 | Select-Object -First 1 | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $versionOutput -match 'GNU bash') {
            return [pscustomobject]@{ Path = $resolved; Version = $versionOutput }
        }
    }

    throw "Git for Windows Bash was not found. WSL bash is intentionally unsupported by this runner."
}

function Wait-ForBrokerReady {
    param(
        [Parameter(Mandatory = $true)][System.Diagnostics.Process]$Process,
        [Parameter(Mandatory = $true)][string]$ReadyPath,
        [Parameter(Mandatory = $true)][string]$FailedPath
    )

    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-Date) -lt $deadline) {
        if (Test-Path -LiteralPath $ReadyPath -PathType Leaf) {
            return Get-Content -LiteralPath $ReadyPath -Raw -Encoding UTF8 | ConvertFrom-Json
        }
        if (Test-Path -LiteralPath $FailedPath -PathType Leaf) {
            $failure = Get-Content -LiteralPath $FailedPath -Raw -Encoding UTF8 | ConvertFrom-Json
            throw "Firewall broker failed: $($failure.error)"
        }
        if ($Process.HasExited) {
            throw "Firewall broker exited before creating the loopback lease (exit $($Process.ExitCode))."
        }
        Start-Sleep -Milliseconds 250
    }
    throw "Timed out waiting for the Windows firewall lease."
}

function Invoke-GoModuleTest {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$ModulePath,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][string]$LogPath,
        [Parameter(Mandatory = $true)][string]$GoPath
    )

    $startedAt = Get-Date
    $commandText = "go " + ($Arguments -join " ")
    Write-Host ""
    Write-Host "=== $Name ===" -ForegroundColor Cyan
    Write-Host "$ModulePath> $commandText"
    $exitCode = 1
    Push-Location $ModulePath
    try {
        & $GoPath @Arguments 2>&1 |
            Tee-Object -FilePath $LogPath |
            ForEach-Object { Write-Host $_ }
        $exitCode = $LASTEXITCODE
    }
    catch {
        $_ | Out-String | Tee-Object -FilePath $LogPath -Append | Write-Host
        $exitCode = 1
    }
    finally {
        Pop-Location
    }

    $elapsed = (Get-Date) - $startedAt
    return [pscustomobject]@{
        Name = $Name
        ModulePath = $ModulePath
        Command = $commandText
        LogPath = $LogPath
        ExitCode = $exitCode
        Passed = ($exitCode -eq 0)
        Duration = $elapsed
    }
}

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not (Test-Path -LiteralPath (Join-Path $repoRoot "go.mod") -PathType Leaf)) {
    throw "Repository root could not be resolved from $PSScriptRoot"
}
if ([string]::IsNullOrWhiteSpace($ResultsDirectory)) {
    $ResultsDirectory = Join-Path $repoRoot "artifacts\windows-native-go-tests"
}
elseif (-not [System.IO.Path]::IsPathRooted($ResultsDirectory)) {
    $ResultsDirectory = Join-Path $repoRoot $ResultsDirectory
}
$resultsRoot = [System.IO.Path]::GetFullPath($ResultsDirectory)
New-Item -ItemType Directory -Path $resultsRoot -Force | Out-Null

$go = Resolve-PinnedGo -RequestedPath $GoExecutable
$gitBash = Resolve-GitBash
$gitBin = Split-Path -Parent $gitBash.Path

Write-Host "Windows native Go test preflight" -ForegroundColor Cyan
Write-Host "Repository : $repoRoot"
Write-Host "Go         : $($go.Path)"
Write-Host "Go version : $($go.Version)"
Write-Host "Git Bash   : $($gitBash.Path)"
Write-Host "Bash       : $($gitBash.Version)"
Write-Host "Results    : $resultsRoot"
Write-Host "WSL        : disabled by strict Git Bash resolution"

if ($PreflightOnly) {
    Write-Host "Preflight passed; no firewall rule was created." -ForegroundColor Green
    exit 0
}

$runToken = [guid]::NewGuid().ToString("N")
$controlDirectory = Join-Path $resultsRoot (".control-" + $runToken)
New-Item -ItemType Directory -Path $controlDirectory -Force | Out-Null
$readyPath = Join-Path $controlDirectory "ready.json"
$failedPath = Join-Path $controlDirectory "failed.json"
$stopPath = Join-Path $controlDirectory "stop"
$donePath = Join-Path $controlDirectory "done.json"
$brokerProcess = $null
$brokerReady = $null
$cleanupPassed = $false
$testResults = New-Object System.Collections.Generic.List[object]
$suiteStartedAt = Get-Date
$originalPath = $env:PATH
$suiteError = ""

try {
    $powerShellExe = Join-Path $PSHOME "powershell.exe"
    $brokerArguments = @(
        "-NoProfile",
        "-ExecutionPolicy", "Bypass",
        "-File", ('"{0}"' -f $PSCommandPath),
        "-FirewallBroker",
        "-BrokerControlDirectory", ('"{0}"' -f $controlDirectory),
        "-ParentProcessId", "$PID",
        "-RunToken", $runToken
    )
    $startArguments = @{
        FilePath = $powerShellExe
        ArgumentList = $brokerArguments
        PassThru = $true
        WindowStyle = "Hidden"
    }
    if (-not (Test-IsAdministrator)) {
        Write-Host "Windows UAC approval is required for a temporary loopback-only firewall lease." -ForegroundColor Yellow
        $startArguments.Verb = "RunAs"
    }
    $brokerProcess = Start-Process @startArguments
    $brokerReady = Wait-ForBrokerReady -Process $brokerProcess -ReadyPath $readyPath -FailedPath $failedPath
    Write-Host "Temporary loopback lease active: $($brokerReady.ruleNames -join ', ')" -ForegroundColor Green

    $env:PATH = "$gitBin;$([System.IO.Path]::GetDirectoryName($go.Path));$originalPath"
    $resolvedBash = (Get-Command bash.exe -ErrorAction Stop).Source
    if (-not [System.IO.Path]::GetFullPath($resolvedBash).Equals($gitBash.Path, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "PATH did not resolve bash.exe to Git Bash: $resolvedBash"
    }
    $env:GOTOOLCHAIN = "local"
    $env:GOPROXY = "https://goproxy.cn,direct"
    $env:GOFLAGS = "-buildvcs=false"
    $env:WINDOWS_SANDBOX_WAIT_MS = "20000"
    Remove-Item Env:REASONIX_RELEASE_CACHE_GUARD -ErrorAction SilentlyContinue

    $testResults.Add((Invoke-GoModuleTest `
        -Name "Root Reasonix" `
        -ModulePath $repoRoot `
        -Arguments @("test", "-p", "4", "-count=1", "-timeout=8m", "./...") `
        -LogPath (Join-Path $resultsRoot "root.log") `
        -GoPath $go.Path))
    $testResults.Add((Invoke-GoModuleTest `
        -Name "Go SDK" `
        -ModulePath (Join-Path $repoRoot "sdk\go") `
        -Arguments @("test", "-p", "4", "-count=1", "-timeout=8m", "./...") `
        -LogPath (Join-Path $resultsRoot "sdk-go.log") `
        -GoPath $go.Path))
    $testResults.Add((Invoke-GoModuleTest `
        -Name "Desktop" `
        -ModulePath (Join-Path $repoRoot "desktop") `
        -Arguments @("test", "-p", "1", "-count=1", "-timeout=12m", "./...") `
        -LogPath (Join-Path $resultsRoot "desktop.log") `
        -GoPath $go.Path))
}
catch {
    $suiteError = $_.Exception.Message
    Write-Host $suiteError -ForegroundColor Red
}
finally {
    $env:PATH = $originalPath
    if ($null -ne $brokerProcess) {
        Write-Utf8File -Path $stopPath -Content @("stop")
        $brokerProcess.WaitForExit(30000) | Out-Null
        if (-not $brokerProcess.HasExited) {
            $suiteError = ($suiteError + " Firewall broker did not stop within 30 seconds.").Trim()
        }
    }
    if (Test-Path -LiteralPath $donePath -PathType Leaf) {
        $brokerDone = Get-Content -LiteralPath $donePath -Raw -Encoding UTF8 | ConvertFrom-Json
        $cleanupPassed = [bool]$brokerDone.success
        if (-not $cleanupPassed) {
            $suiteError = ($suiteError + " Firewall cleanup failed: " + $brokerDone.brokerError + " " + ($brokerDone.cleanupErrors -join "; ")).Trim()
        }
    }
    else {
        $suiteError = ($suiteError + " Firewall broker did not write cleanup evidence.").Trim()
    }
}

$suiteFinishedAt = Get-Date
$summaryPath = Join-Path $resultsRoot "summary.md"
$summary = New-Object System.Collections.Generic.List[string]
$summary.Add("# Windows Native Full Go Test Report")
$summary.Add("")
$summary.Add("- Started: $($suiteStartedAt.ToString('yyyy-MM-dd HH:mm:ss zzz'))")
$summary.Add("- Finished: $($suiteFinishedAt.ToString('yyyy-MM-dd HH:mm:ss zzz'))")
$summary.Add("- Go: ``$($go.Version)``")
$summary.Add("- Bash: ``$($gitBash.Path)`` (Git for Windows; WSL was not used)")
$summary.Add("- Firewall: temporary loopback-only TCP rules; cleanup **$(if ($cleanupPassed) { 'PASS' } else { 'FAIL' })**")
$summary.Add("")
$summary.Add("| Module | Status | Duration | Command | Log |")
$summary.Add("| --- | --- | ---: | --- | --- |")
foreach ($result in $testResults) {
    $status = if ($result.Passed) { "PASS" } else { "FAIL ($($result.ExitCode))" }
    $duration = "{0:mm\:ss}" -f $result.Duration
    $logName = Split-Path -Leaf $result.LogPath
    $summary.Add("| $($result.Name) | **$status** | $duration | ``$($result.Command)`` | [$logName](./$logName) |")
}
if (-not [string]::IsNullOrWhiteSpace($suiteError)) {
    $summary.Add("")
    $summary.Add("## Runner error")
    $summary.Add("")
    $summary.Add($suiteError)
}
$summary.Add("")
$summary.Add("Temporary rule token: ``$runToken``; the broker rechecked each exact rule name after cleanup.")
Write-Utf8File -Path $summaryPath -Content $summary.ToArray()

$allTestsPassed = ($testResults.Count -eq 3)
foreach ($result in $testResults) {
    if (-not $result.Passed) {
        $allTestsPassed = $false
    }
}
$overallPassed = $allTestsPassed -and $cleanupPassed -and [string]::IsNullOrWhiteSpace($suiteError)

if ($null -eq $brokerProcess -or $brokerProcess.HasExited) {
    $controlPrefix = $resultsRoot.TrimEnd([char[]]@('\', '/')) + [System.IO.Path]::DirectorySeparatorChar
    if ($controlDirectory.StartsWith($controlPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        Remove-Item -LiteralPath $controlDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Write-Host ""
Write-Host "Summary: $summaryPath"
if ($overallPassed) {
    Write-Host "All native Windows Go tests passed; temporary firewall rules were removed." -ForegroundColor Green
    exit 0
}
Write-Host "Native Windows Go tests failed. Review the summary and module logs." -ForegroundColor Red
exit 1
