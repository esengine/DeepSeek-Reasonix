param(
  [Parameter(Mandatory=$true)][string]$PortableZip,
  [string]$EvidenceDirectory = (Join-Path $env:TEMP ('reasonix-recovery-' + [guid]::NewGuid().ToString('N')))
)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force $EvidenceDirectory | Out-Null
$install = Join-Path $EvidenceDirectory 'install'
$dataHome = Join-Path $EvidenceDirectory 'home'
if (Test-Path $install) { throw 'Evidence install directory must be new; refusing to overwrite a running fixture.' }
Expand-Archive -LiteralPath $PortableZip -DestinationPath $install
$current = Get-Content (Join-Path $install 'current.json') -Raw | ConvertFrom-Json
$shellPath = Join-Path $install ($current.activeDir + '\app\Reasonix.exe')
$launcher = Join-Path $install 'Reasonix.exe'
$saved = @{}
foreach ($key in @('REASONIX_HOME','REASONIX_NONINTERACTIVE','REASONIX_DEV','REASONIX_DESKTOP_SERVICE','REASONIX_ELECTRON_DEV_URL')) {
  $saved[$key] = [Environment]::GetEnvironmentVariable($key,'Process')
  [Environment]::SetEnvironmentVariable($key,$null,'Process')
}
$env:REASONIX_HOME = $dataHome
$env:REASONIX_NONINTERACTIVE = '1'

function Read-ShellStatus($process) {
  $pipe = [IO.Pipes.NamedPipeClientStream]::new('.', ('reasonix-shell-v1-' + $process.Id), [IO.Pipes.PipeDirection]::In)
  try {
    $pipe.Connect(500)
    $reader = [IO.StreamReader]::new($pipe)
    $read = $reader.ReadLineAsync()
    if (-not $read.Wait(2000)) { return $null }
    if ($read.Result.Length -gt 16384) { throw 'Oversized status response' }
    return ($read.Result | ConvertFrom-Json)
  } catch { return $null } finally { $pipe.Dispose() }
}

function Assert-Ready {
  foreach ($process in @(Get-Process Reasonix -ErrorAction SilentlyContinue)) {
    if ($process.Path -ne $shellPath) { continue }
    $status = Read-ShellStatus $process
    if ($null -eq $status) { continue }
    if ($status.schemaVersion -ne 1 -or $status.product -ne 'com.reasonix.desktop' -or $status.pid -ne $process.Id) { throw 'Status identity mismatch' }
    if ($status.lifecycle -ne 'ready' -or $status.service -ne 'ready' -or -not $status.visible -or -not $status.healthy) { throw ('Not ready: ' + ($status | ConvertTo-Json -Compress)) }
    if ($status.version -ne $current.activeVersion -or $status.rendererVersion -ne $current.activeVersion) { throw 'Target renderer version mismatch' }
    $service = Get-Process -Id $status.servicePID
    $expectedService = Join-Path $install ($current.activeDir + '\reasonix-desktop.exe')
    if ($service.Path -ne $expectedService) { throw 'Service is outside the active release' }
    return @{ Shell=$process; Service=$service; Status=$status }
  }
  throw 'No verified target shell; inspect preserved logs'
}

try {
  $attempt = Start-Process $launcher -PassThru -RedirectStandardOutput (Join-Path $EvidenceDirectory 'launcher.stdout.log') -RedirectStandardError (Join-Path $EvidenceDirectory 'launcher.stderr.log')
  $null = $attempt.Handle
  if (-not $attempt.WaitForExit(40000)) { throw 'Stable launcher timed out; inspect launcher logs' }
  if ($attempt.ExitCode -ne 0) { throw ('Stable launcher failed: exit=' + $attempt.ExitCode + '; ' + (Get-Content (Join-Path $EvidenceDirectory 'launcher.stderr.log') -Raw)) }
  $first = Assert-Ready
  $again = Start-Process $launcher -PassThru
  $null = $again.Handle
  if (-not $again.WaitForExit(40000) -or $again.ExitCode -ne 0) { throw 'Second launch failed or timed out' }
  $second = Assert-Ready
  if ($first.Shell.Id -ne $second.Shell.Id -or $first.Service.Id -ne $second.Service.Id -or $first.Status.generation -ne $second.Status.generation) { throw 'Second launch replaced the healthy instance' }
  Start-Process $shellPath -ArgumentList '--reasonix-lifecycle-request=quit' | Out-Null
  if (-not $first.Shell.WaitForExit(15000) -or -not $first.Service.WaitForExit(1000)) { throw 'Normal exit left a shell or service alive' }
  $shellLog = Get-Content (Join-Path $dataHome 'desktop-shell\logs\shell.log') -Raw
  if ($shellLog -match 'exit deadline exceeded|killing it|termination failed') { throw 'Forced cleanup is not a normal-exit pass' }
  $report = @{
    artifact=(Get-FileHash -Algorithm SHA256 $PortableZip).Hash
    version=$current.activeVersion
    OS=[Environment]::OSVersion.VersionString
    architecture=$env:PROCESSOR_ARCHITECTURE
    shellPID=$first.Shell.Id
    servicePID=$first.Service.Id
    startup='passed'; secondLaunch='passed'; normalExit='passed'
    signing='not checked'; installer='not exercised'; recoveryConsent='not exercised'
  }
  $report | ConvertTo-Json | Set-Content (Join-Path $EvidenceDirectory 'result.json')
  $report | ConvertTo-Json
} finally {
  foreach ($key in $saved.Keys) { [Environment]::SetEnvironmentVariable($key,$saved[$key],'Process') }
  Write-Host "Evidence preserved at $EvidenceDirectory. No forced process cleanup was performed."
}
