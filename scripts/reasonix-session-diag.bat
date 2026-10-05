@echo off
setlocal EnableExtensions
title Reasonix session diagnostics
where powershell >nul 2>&1 || (echo [ERROR] PowerShell not found on this machine. & pause & exit /b 1)
powershell -NoProfile -ExecutionPolicy Bypass -Command "try{[Console]::OutputEncoding=[Text.Encoding]::UTF8}catch{}; $t=Get-Content -LiteralPath '%~f0' -Raw -Encoding UTF8; $i=$t.LastIndexOf('::PSBEGIN'); if($i -lt 0){Write-Host '[ERROR] marker missing, file corrupted in transit'; exit 1}; Invoke-Expression $t.Substring($i+9)"
echo.
pause
exit /b
::PSBEGIN

# ---------------------------------------------------------------
# Reasonix 会话重复（同一会话出现多份）诊断采集
# 只读取元数据，不读取任何聊天正文
# ---------------------------------------------------------------

$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

$script:L = New-Object System.Collections.Generic.List[string]
function W([string]$s = '') { $script:L.Add($s) | Out-Null }

function Fit([object]$v, [int]$w, [switch]$R) {
  $s = [string]$v
  $s = $s -replace '[\r\n\t]+', ' '
  if ($s.Length -gt $w) { $s = $s.Substring(0, [Math]::Max(1, $w - 1)) + '…' }
  if ($R) { return $s.PadLeft($w) }
  return $s.PadRight($w)
}

function ShortTime([object]$v) {
  if (-not $v) { return '' }
  try { return ([datetime]$v).ToUniversalTime().ToString('yyyy-MM-dd HH:mm:ss') } catch { return ([string]$v) }
}

Write-Host ''
Write-Host '  Reasonix 会话重复诊断' -ForegroundColor Cyan
Write-Host '  正在采集，请稍候……'
Write-Host ''

# ---- 输出目录 -------------------------------------------------
$ts = Get-Date -Format 'yyyyMMdd-HHmmss'
$desk = $env:RX_DIAG_OUT   # 内部测试用：设了就写到指定目录、不弹资源管理器
$quiet = -not [string]::IsNullOrWhiteSpace($desk)
if (-not $quiet) { $desk = [Environment]::GetFolderPath('Desktop') }
if ([string]::IsNullOrWhiteSpace($desk)) { $desk = $env:USERPROFILE }
$out = Join-Path $desk "reasonix-session-diag-$ts"
New-Item -ItemType Directory -Path $out -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $out 'conflicts') -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $out 'leases') -Force | Out-Null

# ---- 状态根目录 -----------------------------------------------
$root = $env:REASONIX_STATE_HOME
if ([string]::IsNullOrWhiteSpace($root)) { $root = $env:REASONIX_HOME }
if ([string]::IsNullOrWhiteSpace($root)) { $root = Join-Path $env:APPDATA 'reasonix' }

W '================================================================'
W ' Reasonix 会话重复诊断报告'
W '================================================================'
W ''
W ' 本文件只包含：文件名、大小、时间、内容哈希、会话血缘元数据、会话标题（截断）。'
W ' 不包含任何聊天正文。发送前可自行用记事本检查。'
W ' 表格为便于阅读做了列宽截断，完整未截断数据见同目录 sessions-index.csv。'
W ''
W ("生成时间  : " + (Get-Date -Format 'yyyy-MM-dd HH:mm:ss zzz'))
W ("主机      : " + $env:COMPUTERNAME)
W ("用户      : " + $env:USERNAME)
try { W ("系统      : " + (Get-CimInstance Win32_OperatingSystem -ErrorAction Stop).Caption + " / " + [Environment]::OSVersion.Version) } catch {}
W ("PowerShell: " + $PSVersionTable.PSVersion)
W ("状态根目录: " + $root)
W ("  REASONIX_STATE_HOME = " + $(if ($env:REASONIX_STATE_HOME) { $env:REASONIX_STATE_HOME } else { '(未设置)' }))
W ("  REASONIX_HOME       = " + $(if ($env:REASONIX_HOME) { $env:REASONIX_HOME } else { '(未设置)' }))
W ''
if (-not (Test-Path -LiteralPath $root)) {
  W '[!] 状态根目录不存在，Reasonix 可能安装在别处或使用了自定义 HOME。'
  W ''
}

# ---- [1] 运行中的进程 -----------------------------------------
W '----------------------------------------------------------------'
W ' [1] 正在运行的 Reasonix 进程（判断是否双开）'
W '----------------------------------------------------------------'
try {
  $ps = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.ProcessName -match 'reasonix' })
  if ($ps.Count -gt 0) {
    foreach ($p in $ps) {
      $path = ''; try { $path = $p.Path } catch {}
      $st = ''; try { $st = $p.StartTime.ToString('yyyy-MM-dd HH:mm:ss') } catch {}
      W ("  PID {0,-7} {1,-22} start={2}  {3}" -f $p.Id, $p.ProcessName, $st, $path)
    }
    W ("  共 {0} 个进程" -f $ps.Count)
  } else {
    W '  (当前没有 Reasonix 进程在运行)'
  }
} catch { W ("  读取失败: " + $_.Exception.Message) }
W ''

# ---- 收集会话目录 ---------------------------------------------
$dirs = New-Object System.Collections.Generic.List[object]
$flat = Join-Path $root 'sessions'
if (Test-Path -LiteralPath $flat) { $dirs.Add([pscustomobject]@{ Slug = '(flat)'; Dir = $flat }) | Out-Null }
$projRoot = Join-Path $root 'projects'
if (Test-Path -LiteralPath $projRoot) {
  foreach ($d in (Get-ChildItem -LiteralPath $projRoot -Directory -ErrorAction SilentlyContinue)) {
    $sd = Join-Path $d.FullName 'sessions'
    if (Test-Path -LiteralPath $sd) { $dirs.Add([pscustomobject]@{ Slug = $d.Name; Dir = $sd }) | Out-Null }
  }
}

function Get-JsonlLineCount([string]$path, [long]$size) {
  if ($size -le 0) { return 0 }
  if ($size -gt 80MB) { return -1 }
  $n = 0
  $sr = $null
  try {
    $sr = New-Object System.IO.StreamReader($path)
    while ($null -ne $sr.ReadLine()) { $n++ }
  } catch { $n = -1 } finally { if ($sr) { $sr.Close() } }
  return $n
}

$rows = New-Object System.Collections.Generic.List[object]
$extra = New-Object System.Collections.Generic.List[object]

foreach ($e in $dirs) {
  $files = @()
  try {
    $files = @(Get-ChildItem -LiteralPath $e.Dir -File -Filter *.jsonl -ErrorAction SilentlyContinue |
      Where-Object {
        $_.Extension -eq '.jsonl' -and
        $_.Name -notmatch '\.(events|conflicts|guardian|wire)\.jsonl$' -and
        $_.Name -notlike 'subagent-*'
      })
  } catch {}

  foreach ($f in $files) {
    $id = [System.IO.Path]::GetFileNameWithoutExtension($f.Name)
    $m = $null
    $metaPath = $f.FullName + '.meta'
    if (Test-Path -LiteralPath $metaPath) {
      try { $m = Get-Content -LiteralPath $metaPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {}
    }
    $hash = ''
    if ($f.Length -gt 0) {
      try { $hash = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash.Substring(0, 16) } catch { $hash = '(hash失败)' }
    }
    $title = ''
    if ($m) { if ($m.custom_title) { $title = [string]$m.custom_title } elseif ($m.topic_title) { $title = [string]$m.topic_title } }

    $rows.Add([pscustomobject]@{
      Slug      = $e.Slug
      Dir       = $e.Dir
      Id        = $id
      Title     = $title
      Bytes     = $f.Length
      Lines     = (Get-JsonlLineCount $f.FullName $f.Length)
      SHA256    = $hash
      Parent    = $(if ($m) { [string]$m.parent_id } else { '' })
      Recovered = $(if ($m -and $m.recovered) { 'yes' } else { '' })
      Reason    = $(if ($m) { [string]$m.recovery_reason } else { '' })
      Depth     = $(if ($m -and $m.recovery_depth) { [int]$m.recovery_depth } else { 0 })
      ForkTurn  = $(if ($m -and $m.fork_turn) { [int]$m.fork_turn } else { 0 })
      TopicId   = $(if ($m) { [string]$m.topic_id } else { '' })
      Created   = $(if ($m -and $m.created_at) { [string]$m.created_at } else { $f.CreationTime.ToString('o') })
      Modified  = $f.LastWriteTime.ToString('s')
      HasMeta   = $(if ($m) { 'yes' } else { 'NO' })
    }) | Out-Null
  }

  foreach ($pat in @('*.conflicts.jsonl', '*.events.jsonl.damaged', '*.jsonl.lease.json', '*.jsonl.lock', '*.jsonl.lease.lock')) {
    foreach ($s in (Get-ChildItem -LiteralPath $e.Dir -File -Filter $pat -ErrorAction SilentlyContinue | Where-Object { $_.Name -like $pat })) {
      $extra.Add([pscustomobject]@{ Slug = $e.Slug; Name = $s.Name; Bytes = $s.Length; Modified = $s.LastWriteTime.ToString('s'); Full = $s.FullName }) | Out-Null
    }
  }
}

# ---- [2] 会话清单 ---------------------------------------------
$hdr = (Fit 'Id' 44) + ' ' + (Fit 'Title' 22) + ' ' + (Fit 'Lines' 6 -R) + ' ' + (Fit 'Bytes' 10 -R) + ' ' +
       (Fit 'SHA256-16' 17) + ' ' + (Fit 'Rec' 4) + ' ' + (Fit 'Dep' 4 -R) + ' ' + (Fit 'Fork' 5 -R) + ' ' +
       (Fit 'Parent' 44) + ' ' + (Fit 'TopicId' 20) + ' ' + (Fit 'Created(UTC)' 20) + ' ' + (Fit 'Meta' 4)
$sep = '-' * $hdr.TrimEnd().Length

W '----------------------------------------------------------------'
W ' [2] 会话清单（按项目分组，按创建时间排序）'
W '----------------------------------------------------------------'
if ($rows.Count -eq 0) {
  W '  (没有找到任何会话文件)'
} else {
  W ("  会话文件总数: " + $rows.Count + "   项目目录数: " + $dirs.Count)
  foreach ($g in ($rows | Group-Object Slug | Sort-Object Name)) {
    W ''
    W ("### 项目 slug: " + $g.Name + "    会话数: " + $g.Count)
    W ("    目录: " + $g.Group[0].Dir)
    W ('  ' + $hdr.TrimEnd())
    W ('  ' + $sep)
    foreach ($r in ($g.Group | Sort-Object Created)) {
      $line = (Fit $r.Id 44) + ' ' + (Fit $r.Title 22) + ' ' + (Fit $r.Lines 6 -R) + ' ' + (Fit $r.Bytes 10 -R) + ' ' +
              (Fit $r.SHA256 17) + ' ' + (Fit $r.Recovered 4) + ' ' + (Fit $r.Depth 4 -R) + ' ' + (Fit $r.ForkTurn 5 -R) + ' ' +
              (Fit $r.Parent 44) + ' ' + (Fit $r.TopicId 20) + ' ' + (Fit (ShortTime $r.Created) 20) + ' ' + (Fit $r.HasMeta 4)
      W ('  ' + $line.TrimEnd())
    }
  }
}
W ''

# ---- [3] 疑似重复组 -------------------------------------------
W '----------------------------------------------------------------'
W ' [3] 疑似重复组'
W '----------------------------------------------------------------'

$nonEmpty = @($rows | Where-Object { $_.Bytes -gt 0 -and $_.SHA256 -and $_.SHA256 -ne '(hash失败)' })
$dupHash = @($nonEmpty | Group-Object SHA256 | Where-Object { $_.Count -gt 1 })
W ''
if ($dupHash.Count -gt 0) {
  W ' A. 内容逐字节相同（同一份被复制多次）：'
  foreach ($g in $dupHash) {
    W ("    SHA256 " + $g.Name + "  ×" + $g.Count + "   " + $g.Group[0].Bytes + "B   标题: " + (Fit $g.Group[0].Title 32).TrimEnd())
    foreach ($r in ($g.Group | Sort-Object Created)) { W ("      - " + $r.Id + "   " + (ShortTime $r.Created) + "   [" + $r.Slug + "]") }
  }
} else {
  W ' A. 没有内容逐字节相同的会话文件。'
}

W ''
$dupTitle = @($rows | Where-Object { $_.Title } | Group-Object Slug, Title | Where-Object { $_.Count -gt 1 })
if ($dupTitle.Count -gt 0) {
  W ' B. 同项目内标题相同（内容已各自分叉）：'
  foreach ($g in $dupTitle) {
    W ("    " + $g.Name + "   ×" + $g.Count)
    foreach ($r in ($g.Group | Sort-Object Created)) {
      W ("      - {0}  lines={1}  bytes={2}  recovered={3}  depth={4}  parent={5}  {6}" -f `
          $r.Id, $r.Lines, $r.Bytes, $(if ($r.Recovered) { 'yes' } else { '-' }), $r.Depth, $(if ($r.Parent) { $r.Parent } else { '-' }), (ShortTime $r.Created))
    }
  }
} else {
  W ' B. 没有同项目内标题重复的会话。'
}

W ''
$dupTopic = @($rows | Where-Object { $_.TopicId } | Group-Object TopicId | Where-Object { $_.Count -gt 1 })
if ($dupTopic.Count -gt 0) {
  W ' C. 同一 topic_id 下的多份会话：'
  foreach ($g in $dupTopic) {
    W ("    topic " + $g.Name + "  ×" + $g.Count)
    foreach ($r in ($g.Group | Sort-Object Created)) { W ("      - " + $r.Id + "   lines=" + $r.Lines + "   " + (ShortTime $r.Created) + "   [" + $r.Slug + "]") }
  }
} else {
  W ' C. 没有共享 topic_id 的多份会话。'
}

W ''
$empty = @($rows | Where-Object { $_.Bytes -le 0 })
if ($empty.Count -gt 0) {
  W (' D. 0 字节空会话文件 ' + $empty.Count + ' 个（新建后没写入任何内容，会照样出现在侧边栏）：')
  foreach ($r in ($empty | Sort-Object Created)) { W ("      - " + $r.Id + "   " + (ShortTime $r.Created) + "   [" + $r.Slug + "]") }
} else {
  W ' D. 没有 0 字节空会话文件。'
}
W ''

# ---- [4] 恢复分叉链 -------------------------------------------
W '----------------------------------------------------------------'
W ' [4] 恢复 / 分叉链（parent_id 串联）'
W '----------------------------------------------------------------'
$byId = @{}
foreach ($r in $rows) { $byId[$r.Id] = $r }
$children = @($rows | Where-Object { $_.Parent })
if ($children.Count -gt 0) {
  $isParent = @{}
  foreach ($c in $children) { $isParent[$c.Parent] = $true }
  $leaves = @($children | Where-Object { -not $isParent.ContainsKey($_.Id) })
  foreach ($leaf in $leaves) {
    $chain = @()
    $cur = $leaf
    $guard = 0
    while ($cur -and $guard -lt 32) {
      $chain += ("{0} (lines={1}, recovered={2}, depth={3}, reason={4})" -f `
          $cur.Id, $cur.Lines, $(if ($cur.Recovered) { 'yes' } else { 'no' }), $cur.Depth, $(if ($cur.Reason) { (Fit $cur.Reason 48).TrimEnd() } else { '-' }))
      if ($cur.Parent -and $byId.ContainsKey($cur.Parent)) { $cur = $byId[$cur.Parent] }
      else { if ($cur.Parent) { $chain += ("{0} (父文件已不存在)" -f $cur.Parent) }; break }
      $guard++
    }
    [array]::Reverse($chain)
    W ''
    W ('  ' + ($chain -join ("`r`n     -> ")))
  }
  W ''
  W ("  分叉链条数: " + $leaves.Count + "   带 parent_id 的会话数: " + $children.Count)
} else {
  W '  没有任何会话带 parent_id —— 这些会话都是各自独立创建的，不是恢复分叉产生的。'
}
W ''

# ---- [5] 冲突 / 损坏 / 锁 sidecar -----------------------------
W '----------------------------------------------------------------'
W ' [5] 冲突 / 损坏 / 锁 sidecar'
W '----------------------------------------------------------------'
if ($extra.Count -eq 0) {
  W '  (无)'
} else {
  W ('  ' + (Fit 'Slug' 40) + ' ' + (Fit 'Name' 60) + ' ' + (Fit 'Bytes' 10 -R) + ' ' + (Fit 'Modified' 20))
  foreach ($s in ($extra | Sort-Object Slug, Name)) {
    W ('  ' + ((Fit $s.Slug 40) + ' ' + (Fit $s.Name 60) + ' ' + (Fit $s.Bytes 10 -R) + ' ' + (Fit $s.Modified 20)).TrimEnd())
  }
  foreach ($s in $extra) {
    try {
      if ($s.Name -like '*.conflicts.jsonl' -and $s.Bytes -gt 0) {
        Copy-Item -LiteralPath $s.Full -Destination (Join-Path $out ('conflicts\' + $s.Slug + '__' + $s.Name)) -Force
      } elseif ($s.Name -like '*.lease.json' -and $s.Bytes -gt 0) {
        Copy-Item -LiteralPath $s.Full -Destination (Join-Path $out ('leases\' + $s.Slug + '__' + $s.Name)) -Force
      }
    } catch {}
  }
}
W ''

# ---- [6] 工作区租约 -------------------------------------------
W '----------------------------------------------------------------'
W ' [6] 工作区租约（跨进程写锁）'
W '----------------------------------------------------------------'
$leaseDir = Join-Path $env:LOCALAPPDATA 'reasonix\workspace-leases'
if (Test-Path -LiteralPath $leaseDir) {
  W ("  目录: " + $leaseDir)
  $lf = @(Get-ChildItem -LiteralPath $leaseDir -File -ErrorAction SilentlyContinue)
  $held = @($lf | Where-Object { $_.Length -gt 0 })
  W ("  文件总数: " + $lf.Count + "   非空(疑似仍被持有): " + $held.Count)
  W '  最近修改的 20 个：'
  foreach ($x in ($lf | Sort-Object LastWriteTime -Descending | Select-Object -First 20)) {
    W ("    " + (Fit $x.Name 68) + ' ' + (Fit $x.Length 8 -R) + 'B  ' + $x.LastWriteTime.ToString('yyyy-MM-dd HH:mm:ss'))
  }
  foreach ($x in ($held | Select-Object -First 20)) {
    try { Copy-Item -LiteralPath $x.FullName -Destination (Join-Path $out ('leases\workspace__' + $x.Name)) -Force } catch {}
  }
} else {
  W ("  " + $leaseDir + " 不存在")
}
W ''

# ---- [7] 状态根目录顶层 ---------------------------------------
W '----------------------------------------------------------------'
W ' [7] 状态根目录顶层内容'
W '----------------------------------------------------------------'
try {
  foreach ($x in (Get-ChildItem -LiteralPath $root -ErrorAction SilentlyContinue | Sort-Object Name)) {
    $kind = if ($x.PSIsContainer) { '  <DIR>  ' } else { ('{0,8}B ' -f $x.Length) }
    W ("  " + $kind + " " + (Fit $x.Name 44) + ' ' + $x.LastWriteTime.ToString('yyyy-MM-dd HH:mm:ss'))
  }
} catch { W ("  读取失败: " + $_.Exception.Message) }
W ''

# ---- [8] CLI doctor -------------------------------------------
W '----------------------------------------------------------------'
W ' [8] reasonix CLI 诊断'
W '----------------------------------------------------------------'
$rx = Get-Command reasonix -ErrorAction SilentlyContinue
if ($rx) {
  W ("  CLI 路径: " + $rx.Source)
  try { W ("  版本: " + ((& reasonix --version) -join ' ')) } catch { W '  版本读取失败' }
  try { (& reasonix doctor --json) | Out-File -FilePath (Join-Path $out 'doctor.json') -Encoding utf8; W '  已导出 doctor.json' } catch { W '  doctor --json 失败' }
  try { (& reasonix doctor sessions --json) | Out-File -FilePath (Join-Path $out 'doctor-sessions.json') -Encoding utf8; W '  已导出 doctor-sessions.json' } catch { W '  doctor sessions --json 失败（旧版本没有该子命令属正常）' }
} else {
  W '  未在 PATH 中找到 reasonix CLI（只装了桌面版属正常）。'
  W '  请另外告诉我们桌面端版本号（设置 / 关于 页面）。'
}
W ''
W '================================================================'
W ' 报告结束'
W '================================================================'

# ---- 落盘 -----------------------------------------------------
$reportPath = Join-Path $out 'report.txt'
($script:L -join "`r`n") | Out-File -FilePath $reportPath -Encoding utf8
try {
  $rows | Sort-Object Slug, Created |
    Select-Object Slug, Id, Title, Bytes, Lines, SHA256, HasMeta, Parent, Recovered, Reason, Depth, ForkTurn, TopicId, Created, Modified, Dir |
    Export-Csv -Path (Join-Path $out 'sessions-index.csv') -NoTypeInformation -Encoding UTF8
} catch {}

$zip = "$out.zip"
$ok = $false
try {
  Add-Type -AssemblyName System.IO.Compression.FileSystem -ErrorAction Stop
  if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
  [System.IO.Compression.ZipFile]::CreateFromDirectory($out, $zip)
  $ok = $true
} catch {
  try { Compress-Archive -Path (Join-Path $out '*') -DestinationPath $zip -Force; $ok = $true } catch {}
}
if ($ok) { try { Remove-Item -LiteralPath $out -Recurse -Force } catch {} }

Write-Host ''
Write-Host ('  扫描到 ' + $rows.Count + ' 个会话文件，' + $dirs.Count + ' 个项目目录')
if ($dupHash.Count -gt 0) { Write-Host ('  发现内容完全相同的重复组 ' + $dupHash.Count + ' 组') -ForegroundColor Yellow }
if ($dupTitle.Count -gt 0) { Write-Host ('  发现同名会话组 ' + $dupTitle.Count + ' 组') -ForegroundColor Yellow }
Write-Host ''
if ($ok) {
  Write-Host '  采集完成 ✔' -ForegroundColor Green
  Write-Host ''
  Write-Host ('  结果文件: ' + $zip) -ForegroundColor Yellow
  Write-Host ''
  Write-Host '  这个 zip 里只有元数据，没有聊天正文，可以直接发给我们。'
  Write-Host '  （里面的 report.txt 可以用记事本打开自行检查）'
  if (-not $quiet) { try { Start-Process explorer.exe ('/select,"' + $zip + '"') } catch {} }
} else {
  Write-Host '  打包失败，请手动把这个文件夹压缩后发给我们：' -ForegroundColor Yellow
  Write-Host ('  ' + $out)
}
Write-Host ''
Write-Host '  另外麻烦回答三个问题：'
Write-Host '   1) 出问题时是不是同时开着两个 Reasonix（桌面端 + 命令行，或两个窗口开同一项目）？'
Write-Host '   2) 这些重复是一直都在，还是某次操作之后突然冒出来的（中断生成 / 切模型 / 断网重连 / 升级）？'
Write-Host '   3) 桌面端版本号是多少？'
Write-Host ''
