# Installs a Windows installer where it is meant to run, silently and per user,
# checks what landed, and uninstalls. The package check unpacks with the
# runner's modern 7-Zip, which reads archives the NSIS extractor cannot, so only
# running the installer shows that it installs every executable. Holds no
# credentials: it runs on pull requests against the unsigned installer and in
# the release against the signed one.
param(
    [Parameter(Mandatory = $true)]
    [string]$Installer,

    [Parameter(Mandatory = $true)]
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "windows-signing-lib.ps1")

$temp = if ($env:RUNNER_TEMP) { $env:RUNNER_TEMP } else { [IO.Path]::GetTempPath() }
$target = Join-Path $temp ("rx-install-" + [guid]::NewGuid().ToString("N"))
$env:REASONIX_HOME = Join-Path $temp "reasonix-home"

$run = Start-Process -FilePath (Resolve-Path -LiteralPath $Installer).Path -ArgumentList "/S", "/D=$target" -Wait -PassThru
if ($run.ExitCode -ne 0) { throw "the installer exited $($run.ExitCode)" }

# The installer can exit before its files are on disk (real-time scanning holds
# them), so the tree is read once the application is there and the file count
# has stopped moving.
function Wait-InstallSettled {
    $clock = [Diagnostics.Stopwatch]::StartNew()
    $last = -1
    $stable = 0
    while ($clock.Elapsed.TotalSeconds -lt 120) {
        $count = @(Get-ChildItem -LiteralPath $target -Recurse -File -Force -ErrorAction SilentlyContinue).Count
        if ((Test-Path -LiteralPath (Join-Path $target "Reasonix Studio.exe")) -and $count -eq $last) { $stable++ } else { $stable = 0 }
        if ($stable -ge 3) { break }
        $last = $count
        Start-Sleep -Seconds 2
    }
    Write-Host "install settled $([int]$clock.Elapsed.TotalSeconds)s after the installer exited, $last files"
}
Wait-InstallSettled
if (-not (Test-Path -LiteralPath (Join-Path $target "Reasonix Studio.exe"))) {
    Write-Host "target holds: $((Get-ChildItem -Force $target -ErrorAction SilentlyContinue | ForEach-Object Name) -join ', ')"
    throw "the installer reported success and installed nothing"
}

# Every executable the release declares is on disk, and nothing else that is a
# PE image besides what the installer writes itself.
$found = @(Get-PeFiles -Root $target | Where-Object { $script:InstallerWrittenX86Pe -cnotcontains $_ })
$declared = @($script:ReleaseSignedPe + $script:MicrosoftSignedPe)
$diff = @(Compare-Object -CaseSensitive ($declared | Sort-Object -CaseSensitive) ($found | Sort-Object -CaseSensitive))
if ($diff.Count -gt 0) {
    foreach ($d in $diff) { Write-Host "::error title=studio-install.pe-set::$($d.InputObject) $($d.SideIndicator)" }
    throw "the installed PE set differs from the declared one"
}
foreach ($owned in $script:InstallerWrittenX86Pe) {
    if (-not (Test-Path -LiteralPath (Join-Path $target $owned))) { throw "the installer did not write $owned" }
}
Assert-PeArchitecture -Root $target -Architecture $Architecture

$env:ELECTRON_RUN_AS_NODE = "1"
$out = Join-Path $temp "shell-arch.txt"
$node = Start-Process -FilePath (Join-Path $target "Reasonix Studio.exe") -ArgumentList "-p", "process.arch" -RedirectStandardOutput $out -Wait -PassThru
Remove-Item Env:ELECTRON_RUN_AS_NODE
$want = if ($Architecture -eq "amd64") { "x64" } else { $Architecture }
$got = (Get-Content -Raw $out).Trim()
if ($node.ExitCode -ne 0 -or $got -ne $want) { throw "the installed shell reports '$got' (exit $($node.ExitCode)), want $want" }
& (Join-Path $target "resources/bin/reasonix-studio-host.exe") -instance-id
if ($LASTEXITCODE -ne 0) { throw "the installed kernel exited $LASTEXITCODE" }

$run = Start-Process -FilePath (Join-Path $target "Uninstall Reasonix Studio.exe") -ArgumentList "/S", "_?=$target" -Wait -PassThru
if ($run.ExitCode -ne 0) { throw "the uninstaller exited $($run.ExitCode)" }
if (Test-Path -LiteralPath (Join-Path $target "Reasonix Studio.exe")) { throw "the uninstaller left the application behind" }
Write-Host "Installed, ran and removed the $Architecture installer."
