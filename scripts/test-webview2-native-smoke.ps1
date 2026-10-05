[CmdletBinding(DefaultParameterSetName = "Run")]
param(
    [Parameter(Mandatory = $true, ParameterSetName = "Run")]
    [string]$ExecutablePath,
    [Parameter(Mandatory = $true, ParameterSetName = "SelfTest")]
    [switch]$SelfTest,
    [Parameter(ParameterSetName = "Run")]
    [int]$TimeoutSeconds = 60,
    [Parameter(ParameterSetName = "Run")]
    [int]$HealthySeconds = 5
)

$ErrorActionPreference = "Stop"

function Get-DescendantProcesses {
    param([int]$RootProcessId)

    $snapshot = @(Get-CimInstance Win32_Process | Select-Object ProcessId, ParentProcessId, Name, CommandLine)
    $pending = @([uint32]$RootProcessId)
    $seen = @{}
    $descendants = @()
    while ($pending.Count -gt 0) {
        $parentProcessId = [uint32]$pending[0]
        $pending = if ($pending.Count -gt 1) { @($pending[1..($pending.Count - 1)]) } else { @() }
        foreach ($child in @($snapshot | Where-Object { $_.ParentProcessId -eq $parentProcessId })) {
            $childProcessId = [uint32]$child.ProcessId
            if ($seen.ContainsKey($childProcessId)) {
                continue
            }
            $seen[$childProcessId] = $true
            $descendants += $child
            $pending += $childProcessId
        }
    }
    return @($descendants)
}

function Get-WebViewAutomationState {
    param([IntPtr]$WindowHandle)

    if ($WindowHandle -eq [IntPtr]::Zero) {
        return [pscustomobject]@{
            DocumentReady = $false
            ComposerReady = $false
            ErrorType = ""
        }
    }

    try {
        $root = [System.Windows.Automation.AutomationElement]::FromHandle($WindowHandle)
        if ($null -eq $root) {
            return [pscustomobject]@{
                DocumentReady = $false
                ComposerReady = $false
                ErrorType = ""
            }
        }
        $documentCondition = [System.Windows.Automation.PropertyCondition]::new(
            [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
            [System.Windows.Automation.ControlType]::Document
        )
        $composerTypeCondition = [System.Windows.Automation.PropertyCondition]::new(
            [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
            [System.Windows.Automation.ControlType]::Edit
        )
        $composerIdCondition = [System.Windows.Automation.PropertyCondition]::new(
            [System.Windows.Automation.AutomationElement]::AutomationIdProperty,
            "composer-input"
        )
        $composerCondition = [System.Windows.Automation.AndCondition]::new(
            [System.Windows.Automation.Condition[]]@($composerTypeCondition, $composerIdCondition)
        )
        return [pscustomobject]@{
            DocumentReady = $null -ne $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $documentCondition)
            ComposerReady = $null -ne $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $composerCondition)
            ErrorType = ""
        }
    }
    catch {
        return [pscustomobject]@{
            DocumentReady = $false
            ComposerReady = $false
            ErrorType = $_.Exception.GetType().Name
        }
    }
}

function Get-NativeSmokeState {
    param([System.Diagnostics.Process]$Process)

    $Process.Refresh()
    $exited = $Process.HasExited
    $windowHandle = if ($exited) { [IntPtr]::Zero } else { $Process.MainWindowHandle }
    $descendants = @(Get-DescendantProcesses -RootProcessId $Process.Id)
    $renderer = @($descendants | Where-Object {
        $_.Name -ieq "msedgewebview2.exe" -and $_.CommandLine -match "--type=renderer"
    })
    $automation = Get-WebViewAutomationState -WindowHandle $windowHandle
    return [pscustomobject]@{
        Exited = $exited
        WindowHandle = $windowHandle
        RendererCount = $renderer.Count
        DocumentReady = $automation.DocumentReady
        ComposerReady = $automation.ComposerReady
        AutomationErrorType = $automation.ErrorType
        Descendants = @($descendants | ForEach-Object { "$($_.Name)[$($_.ProcessId)]" })
    }
}

function Test-NativeSmokeHealthy {
    param([object]$State)

    return -not $State.Exited `
        -and $State.WindowHandle -ne [IntPtr]::Zero `
        -and $State.RendererCount -gt 0 `
        -and $State.DocumentReady `
        -and $State.ComposerReady
}

function Update-NativeSmokeStability {
    param(
        [hashtable]$Tracker,
        [object]$State,
        [DateTime]$Now,
        [int]$RequiredHealthySeconds
    )

    if ($State.Exited) {
        return "Exited"
    }
    if (-not (Test-NativeSmokeHealthy -State $State)) {
        $Tracker["HealthySince"] = $null
        return "Waiting"
    }
    if ($null -eq $Tracker["HealthySince"]) {
        $Tracker["HealthySince"] = $Now
        return "Waiting"
    }
    if (($Now - $Tracker["HealthySince"]).TotalSeconds -ge $RequiredHealthySeconds) {
        return "Ready"
    }
    return "Waiting"
}

function Format-NativeSmokeState {
    param([object]$State)

    if ($null -eq $State) {
        return "unavailable"
    }
    $window = "0x{0:X}" -f $State.WindowHandle.ToInt64()
    return "exited=$($State.Exited) window=$window renderer=$($State.RendererCount) document=$($State.DocumentReady) composer=$($State.ComposerReady) automation_error=$($State.AutomationErrorType) descendants=$($State.Descendants -join ', ')"
}

function Assert-NativeSmokeSelfTest {
    param(
        [bool]$Condition,
        [string]$Message
    )

    if (-not $Condition) {
        throw "native smoke self-test failed: $Message"
    }
}

function New-NativeSmokeSelfTestState {
    param(
        [bool]$Exited = $false,
        [bool]$WindowReady = $true,
        [int]$RendererCount = 1,
        [bool]$DocumentReady = $true,
        [bool]$ComposerReady = $true
    )

    return [pscustomobject]@{
        Exited = $Exited
        WindowHandle = if ($WindowReady) { [IntPtr]1 } else { [IntPtr]::Zero }
        RendererCount = $RendererCount
        DocumentReady = $DocumentReady
        ComposerReady = $ComposerReady
    }
}

function Invoke-NativeSmokeStateMachineSelfTest {
    $start = [DateTime]::Parse("2026-01-01T00:00:00Z").ToUniversalTime()
    $tracker = @{ HealthySince = $null }
    $healthy = New-NativeSmokeSelfTestState
    $missingRenderer = New-NativeSmokeSelfTestState -RendererCount 0

    $result = Update-NativeSmokeStability -Tracker $tracker -State $healthy -Now $start -RequiredHealthySeconds 5
    Assert-NativeSmokeSelfTest ($result -eq "Waiting") "the first healthy sample must start, not finish, the stability window"
    $result = Update-NativeSmokeStability -Tracker $tracker -State $missingRenderer -Now ($start.AddSeconds(4)) -RequiredHealthySeconds 5
    Assert-NativeSmokeSelfTest ($result -eq "Waiting" -and $null -eq $tracker["HealthySince"]) "a transient renderer handoff must reset without failing"
    $result = Update-NativeSmokeStability -Tracker $tracker -State $healthy -Now ($start.AddSeconds(5)) -RequiredHealthySeconds 5
    Assert-NativeSmokeSelfTest ($result -eq "Waiting") "health after a handoff must begin a new stability window"
    $result = Update-NativeSmokeStability -Tracker $tracker -State $healthy -Now ($start.AddSeconds(10)) -RequiredHealthySeconds 5
    Assert-NativeSmokeSelfTest ($result -eq "Ready") "five consecutive healthy seconds must pass"

    foreach ($state in @(
        (New-NativeSmokeSelfTestState -WindowReady $false),
        (New-NativeSmokeSelfTestState -DocumentReady $false),
        (New-NativeSmokeSelfTestState -ComposerReady $false)
    )) {
        $tracker["HealthySince"] = $start
        $result = Update-NativeSmokeStability -Tracker $tracker -State $state -Now ($start.AddSeconds(5)) -RequiredHealthySeconds 5
        Assert-NativeSmokeSelfTest ($result -eq "Waiting" -and $null -eq $tracker["HealthySince"]) "window, document, and composer readiness must all be required"
    }
    $result = Update-NativeSmokeStability -Tracker @{ HealthySince = $null } -State (New-NativeSmokeSelfTestState -Exited $true) -Now $start -RequiredHealthySeconds 5
    Assert-NativeSmokeSelfTest ($result -eq "Exited") "process exit must remain terminal"
    Write-Host "WebView2 native smoke state-machine self-test passed"
}

if ($SelfTest) {
    Invoke-NativeSmokeStateMachineSelfTest
    exit 0
}

if ($TimeoutSeconds -le 0 -or $HealthySeconds -le 0 -or $HealthySeconds -ge $TimeoutSeconds) {
    throw "TimeoutSeconds must be positive and greater than HealthySeconds"
}

Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes

$exe = (Resolve-Path $ExecutablePath).Path
$tempRoot = if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$smokeRoot = Join-Path $tempRoot ("reasonix-webview2-native-" + [guid]::NewGuid().ToString("N"))
$smokeHome = Join-Path $smokeRoot "home"
$smokeState = Join-Path $smokeRoot "state"
$smokeCache = Join-Path $smokeRoot "cache"
New-Item -ItemType Directory -Path $smokeHome, $smokeState, $smokeCache | Out-Null
Set-Content -LiteralPath (Join-Path $smokeHome "config.toml") -Encoding utf8 -Value @"
[desktop]
close_behavior = "quit"
"@

$oldHome = $env:REASONIX_HOME
$oldStateHome = $env:REASONIX_STATE_HOME
$oldCacheHome = $env:REASONIX_CACHE_HOME
$process = $null
try {
    $env:REASONIX_HOME = $smokeHome
    $env:REASONIX_STATE_HOME = $smokeState
    $env:REASONIX_CACHE_HOME = $smokeCache
    $process = Start-Process -FilePath $exe -WorkingDirectory (Split-Path $exe) -PassThru

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $tracker = @{ HealthySince = $null }
    $readyState = $null
    $lastState = $null
    while ([DateTime]::UtcNow -lt $deadline) {
        $state = Get-NativeSmokeState -Process $process
        $lastState = $state
        if ($state.Exited) {
            throw "Reasonix exited before the native window became healthy (exit code $($process.ExitCode))"
        }
        $stability = Update-NativeSmokeStability -Tracker $tracker -State $state -Now ([DateTime]::UtcNow) -RequiredHealthySeconds $HealthySeconds
        if ($stability -eq "Ready") {
            $readyState = $state
            break
        }
        Start-Sleep -Milliseconds 250
    }
    if ($null -eq $readyState) {
        throw "Reasonix did not keep its main window, WebView2 renderer, document, and composer healthy for $HealthySeconds consecutive seconds within $TimeoutSeconds seconds; last_state=$(Format-NativeSmokeState -State $lastState)"
    }

    if (-not $process.CloseMainWindow()) {
        throw "Reasonix main window rejected the graceful close request"
    }
    if (-not $process.WaitForExit(10000)) {
        throw "Reasonix did not exit within 10 seconds after the graceful close request"
    }
    if ($process.ExitCode -ne 0) {
        throw "Reasonix exited with code $($process.ExitCode) after the graceful close request"
    }

    Write-Host "Wails/WebView2 native startup smoke passed (window + renderer + document + composer healthy for $HealthySeconds consecutive seconds)"
}
finally {
    $env:REASONIX_HOME = $oldHome
    $env:REASONIX_STATE_HOME = $oldStateHome
    $env:REASONIX_CACHE_HOME = $oldCacheHome
    if ($null -ne $process -and -not $process.HasExited) {
        & taskkill.exe /PID $process.Id /T /F 2>$null | Out-Null
    }
    if (Test-Path $smokeRoot) {
        Remove-Item -LiteralPath $smokeRoot -Recurse -Force
    }
}
