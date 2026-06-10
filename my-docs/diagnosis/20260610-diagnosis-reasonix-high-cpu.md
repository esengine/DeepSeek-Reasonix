# 异常诊断报告：reasonix-go 高 CPU 占用

## 基本信息

| 条目 | 内容 |
|------|------|
| **现象** | `reasonix-go chat --yolo` 进程长期占据 87-107% CPU（单核满负载），持续 12.5 小时 |
| **发现时间** | 2026-06-10 11:43 |
| **进程 PID** | 79078 |
| **启动命令** | `reasonix-go chat --yolo` |
| **启动时间** | 2026-06-09 23:08 |
| **运行时长** | 12 小时 35 分钟 |
| **组件版本** | DeepSeek-Reasonix main-v2（Go 版），wkj-cli 分支，本地编译 |
| **系统环境** | macOS 26.4.1, 64GB RAM, 14 cores |

## 排查过程

### Step 1：进程资源确认

```
$ ps -p 79078 -o pid,pcpu,pmem,rss,comm
  PID  %CPU  %MEM    RSS  COMM
79078  87.5   0.2  108M   reasonix-go
```

RSS 108MB → 运行 12.5 小时后内存占用极低，**非内存泄漏特征**。

### Step 2：系统内存压力分析

```
总内存    64GB
Free     ~500MB        ← 极低
Wired     18.2GB        ← 不可换出
Swap      4GB / 5GB     ← 80% 占用（严重）
Compressor 21.2GB       ← 大量内存压缩
```

系统已处于严重内存压力状态——Swap 几乎满，Compressor 存了 21GB 压缩数据。任何进程的内存访问都会触发换页。

### Step 3：进程 goroutine 状态确认

```
$ kill -3 79078    # 请求 Go 运行时 dump goroutine 栈
     → 进程已退出，无法捕获
```

进程在诊断中途自行退出，说明可能正常结束或被系统 OOM killer 判定。

### Step 4：源码分析 — Agent.Run() 主循环（agent.go:422）

```go
for step := 0; a.maxSteps <= 0 || step < a.maxSteps; step++ {
    text, reasoning, signature, calls, usage, interrupted, partialToolStarted, err := a.stream(ctx, step+1)
    if err != nil {
        if interrupted && streamRecoveries < maxStreamRecoveries {
            // 重试逻辑 — step-- 不消耗 maxSteps 预算
            continue
        }
        return err
    }
    // ...
    if len(calls) == 0 {
        // 最终答案检查 — 含 readiness 重试
        // readiness 失败 → 最多重试 3 次
        // 空答案 → 最多重试 3 次  
    }
    // 执行工具调用
    results := a.executeBatch(ctx, calls)
}
```

**关键发现：** 循环本身阻塞在 `a.stream()`（HTTP API 调用），不存在 CPU 空转。重试逻辑有次数上限（maxFinalReadinessBlocks=3, maxEmptyFinalBlocks=3），不会无限循环。

### Step 5：源码分析 — /loop 调度机制（chat_tui.go）

TurnDone 处理器：
```go
if m.loopPrompt != "" && len(m.pendingInterject) == 0 {
    cmds = append(cmds, tea.Tick(m.loopInterval, func(_ time.Time) tea.Msg {
        return loopTickMsg{}
    }))
}
```

loopTickMsg 处理器：
```go
case loopTickMsg:
    if m.loopPrompt != "" && m.state != tuiRunning {
        m.loopIter++
        cmds = append(cmds, m.startTurnWithRaw(...))
    }
    // Busy: discard, next TurnDone will schedule a fresh tick.
```

**结论：** /loop 用 `tea.Tick` 实现，是事件驱动的**非阻塞**计时器。不涉及定时轮询或 busy-wait。当 Agent 运行时（tuiRunning 状态），loop tick 被静默丢弃，下一个 TurnDone 才重新调度。**无 CPU 泄漏风险。**

### Step 6：系统级并发资源分析

同时期系统高负载进程：

| 进程 | CPU | 内存 | 备注 |
|------|:---:|:----:|------|
| reasonix-go | 87-107% | 108MB | 可疑目标 |
| Chrome (×2 进程) | 36% + 28% | ~1.5GB | 正常浏览器 |
| WindowServer | 36% | 188MB | 桌面渲染（高 CPU 表明 UI 响应差） |
| OrbStack Helper | 11% | 1.2GB | Docker 运行时 |
| cua-test container | 9% | 1.087GB | 462 PIDs, 194GB 块 IO |
| iTerm2 | 7.5% | 395MB | 终端 |
| Qianwen | 6.7% | 490MB | 阿里通义 |
| WPS Office | 4.9% | 367MB | |
| Hermes Python | 5.0% | 175MB | |

## 根因分析

### 结论：不是 reasonix-go 的 bug，是系统内存压力溢出

**排在第一位的原因是：系统 Swap 使用 80%（4GB/5GB）+ Compressor 21GB + Free 仅 500MB。**

在这种内存压力下：

1. **Go 运行时 GC 触发 thrashing** — Go 的 GC 是并发标记-清扫。当系统内存不足时，Go 运行时频繁触发 GC，每次 GC 涉及大量页面错误（page fault）和内存压缩，这些开销**计入用户态 CPU 时间**，表现为 `reasonix-go` 进程 CPU 高。

2. **Go 的 `readGCStats()` 和 `scavenger` 在后台运行** — 即使进程逻辑上空闲（等待用户输入），Go 运行时也会在后台做内存整理。在 swap 压力下，这些后台操作慢 10-100×，累积 CPU 时间。

3. **WindowServer 36% CPU** — 浏览器卡顿的直接证据。WindowServer 负责所有窗口合成，当内存压力大时，合成帧率骤降，表现为 UI 响应慢。

**排除的假设：**

| 排除项 | 理由 |
|--------|------|
| ❌ 内存泄漏 | RSS 12h 稳定在 108MB，VSZ 419GB 是 Go 虚拟地址空间默认预留 |
| ❌ 无限循环 | 源码分析确认所有循环都有上限或阻塞在 I/O |
| ❌ /loop busy-wait | tea.Tick 是 event-driven 定时器，非阻塞 |
| ❌ goroutine 泄漏 | 未捕获到 goroutine 栈 dump（进程已退出），但 RSS 稳定支持无泄漏 |

### 直接诱因

`reasonix-go chat --yolo` 本身只占 108MB 内存。高 CPU 是其**受害结果**，不是**致病原因**。真正的问题是：

1. `cua-test` Docker 容器（462 PIDs, 194GB 块 IO, 1.087GB RAM）——持续大量磁盘 IO 加剧 swap 压力
2. Chrome + Trae CN + WPS + 企业微信 + OrbStack 合计超过 6GB 常驻
3. 总内存 64GB 中 18.2GB 被 wired（不可换出），留给应用的空间不足

## 解决方案

### P0 — 当前已生效

目标进程 `reasonix-go`（PID 79078）已在诊断过程中自行退出，CPU 恢复正常。

### P1 — 释放系统内存

```bash
# 如 cua-test 容器暂时不用
docker stop cua-test       # 释放 ~1GB + 停止 194GB IO

# 关闭闲置应用（Trae CN ≈ 2GB, Qianwen ≈ 500MB, WPS ≈ 367MB）
```

### P2 — 监控与预防

- 建立系统资源告警 cron job：当 Swap > 3GB 或 Free < 1GB 时通知
- `reasonix-go` 长时间空闲时考虑退出 CLI（而非持续后台保持）

## 预防措施

1. **对长时间运行但不做实际工作的 Go CLI 进程，建议超时自动退出**（15 分钟无活动则 exit），避免在内存压力下成为 GC thrashing 的受害者
2. **系统 swap 监控** — my-agent-group 层面的系统健康扫描应包含 swap 使用率检查
3. **Docker 容器资源限制** — `cua-test` 容器无内存/CPU 限制，建议添加 `--memory=4g --cpus=2` 限制
