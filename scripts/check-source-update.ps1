[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$CliPath,

    [Parameter(Mandatory = $true)]
    [string]$SourceRoot,

    [Parameter(Mandatory = $true)]
    [string]$ResultPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function ConvertTo-WindowsProcessArgument {
    param([Parameter(Mandatory = $true)][string]$Value)

    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') {
        return $Value
    }
    $escaped = $Value -replace '(\\*)"', '$1$1\"'
    $escaped = $escaped -replace '(\\+)$', '$1$1'
    return '"' + $escaped + '"'
}

function Invoke-SourceUpdateCli {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][string]$Root
    )

    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = [IO.Path]::GetFullPath($Executable)
    $arguments = @(
        'source-update',
        '--check',
        '--root',
        $Root,
        '--json'
    ) | ForEach-Object { ConvertTo-WindowsProcessArgument $_ }
    $startInfo.Arguments = $arguments -join ' '
    $startInfo.WorkingDirectory = [IO.Path]::GetDirectoryName($startInfo.FileName)
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $startInfo
    try {
        if (-not $process.Start()) {
            throw "无法启动源码更新 CLI：$($startInfo.FileName)"
        }
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        $process.WaitForExit()
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            Stdout = $stdoutTask.GetAwaiter().GetResult()
            Stderr = $stderrTask.GetAwaiter().GetResult()
        }
    }
    finally {
        $process.Dispose()
    }
}

function Write-SourceUpdateResult {
    param(
        [Parameter(Mandatory = $true)][string]$Json,
        [Parameter(Mandatory = $true)][string]$Path
    )

    $parent = Split-Path -Parent ([IO.Path]::GetFullPath($Path))
    if ($parent) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText([IO.Path]::GetFullPath($Path), $Json.Trim() + [Environment]::NewLine, $utf8)
}

function Get-SourceUpdateExitCode {
    param([Parameter(Mandatory = $true)][string]$Status)

    switch ($Status) {
        'up-to-date' { return 0 }
        'upstream-update' { return 10 }
        'baseline-missing' { return 11 }
        'check-failed' { return 12 }
        default { return 2 }
    }
}

if (-not (Test-Path -LiteralPath $CliPath -PathType Leaf)) {
    Write-Warning "源码更新检查未执行：自定义 CLI 不存在：$CliPath"
    exit 2
}
if (-not (Test-Path -LiteralPath $SourceRoot -PathType Container)) {
    Write-Warning "源码更新检查未执行：源码目录不存在：$SourceRoot"
    exit 2
}

$invocation = Invoke-SourceUpdateCli -Executable $CliPath -Root $SourceRoot
$json = $invocation.Stdout.Trim()
if ($json.Length -eq 0) {
    Write-Warning "源码更新检查失败：CLI 没有返回 JSON；退出码 $($invocation.ExitCode)"
    if ($invocation.Stderr.Trim()) {
        Write-Warning $invocation.Stderr.Trim()
    }
    exit 2
}

try {
    $result = $json | ConvertFrom-Json
}
catch {
    Write-Warning "源码更新检查失败：CLI 返回的内容不是有效 JSON"
    exit 2
}

Write-SourceUpdateResult -Json $json -Path $ResultPath
$status = ([string]$result.status).Trim()
switch ($status) {
    'up-to-date' {
        Write-Output "源码更新检查：当前本地 main-v2 基线无变化。"
    }
    'upstream-update' {
        Write-Warning "检测到上游 main-v2 源码变化；仅提示，不拉取、不合并、不安装。"
    }
    'baseline-missing' {
        Write-Warning "源码更新检查无法比较：本地 origin/main-v2 基线缺失；未执行 fetch。"
    }
    'check-failed' {
        Write-Warning "源码更新检查失败；不把失败视为无更新，桌面端将继续启动。"
    }
    default {
        Write-Warning "源码更新检查返回未知状态：$status"
    }
}
$message = ([string]$result.message).Trim()
if ($message) {
    Write-Output ("源码更新说明：{0}" -f $message)
}
exit (Get-SourceUpdateExitCode -Status $status)
