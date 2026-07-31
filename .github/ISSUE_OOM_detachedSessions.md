---
title: "[Bug]: detachedSessions never GC'd — OOM when switching tabs/sessions over time"
labels: ["bug", "desktop", "v2"]
---

## 问题描述 (Description)

### 中文

Reasonix 桌面版在长期使用后会被系统 OOM killer 杀死，系统提示"设备内存接近占满，Reasonix 使用了大量内存已被强制停止"。

**根因定位：**

`desktop/app.go:152` 新增的 `detachedSessions map[string]*WorkspaceTab` 在用户切换 tab 或 session 时，将完整的 Controller（包含全部消息历史、provider 连接、tool registry 等）移入该 map 而不是销毁它。这些 detach 的 session **从不自动清理**——没有超时机制，没有 LRU 驱逐，没有自动 GC。清理仅发生在 `DeleteSession` / `DeleteTopicRuntime` / `RemoveWorkspace` 这三个操作中。

随着用户日常使用来回切换 tab，`detachedSessions` 中积累了大量完整的 agent 运行时，每个持有自己的 `[]provider.Message`（全部对话历史），最终吃光系统内存。

**关键代码路径：**

- `desktop/app.go:150-152` — map 定义和注释
- `desktop/tabs.go:424-452` — `detachSessionRuntime()`：将 tab 移入 detachedSessions
- `desktop/tabs.go:462-549` — `cloneDetachedRuntimeTab()` / `detachRuntimeForReplacement()`：克隆运行时
- 清理仅发生在 `desktop/app.go:2550-2559`（DeleteSession）、`desktop/app.go:2607-2618`（DeleteTopicRuntime）、`desktop/app.go:4005-4010`（RemoveWorkspace）

**系统环境：** Linux (64GB RAM), desktop mode

### English

The Reasonix desktop app gets killed by the OS OOM killer after extended use, showing "Device memory is nearly full, Reasonix has been forcefully stopped."

**Root cause:**

`desktop/app.go:152` introduced `detachedSessions map[string]*WorkspaceTab`. When the user switches tabs or sessions, the full Controller (with all conversation history, provider connections, tool registry, etc.) is moved into this map instead of being destroyed. These detached sessions **are never automatically cleaned up** — no timeout, no LRU eviction, no automatic GC. Cleanup only happens in three operations: `DeleteSession`, `DeleteTopicRuntime`, and `RemoveWorkspace`.

As users switch tabs in daily use, `detachedSessions` accumulates many complete agent runtimes, each holding its own `[]provider.Message` with full conversation history, eventually exhausting system memory.

**Key code paths:**
- `desktop/app.go:150-152` — map definition
- `desktop/tabs.go:424-452` — `detachSessionRuntime()`: moves tab into detachedSessions
- `desktop/tabs.go:462-549` — `cloneDetachedRuntimeTab()` / `detachRuntimeForReplacement()`: clones runtime
- Cleanup only in: `desktop/app.go:2550-2559` (DeleteSession), `desktop/app.go:2607-2618` (DeleteTopicRuntime), `desktop/app.go:4005-4010` (RemoveWorkspace)

**Environment:** Linux (64GB RAM), desktop mode

## 复现步骤 (Steps to Reproduce)

1. Open Reasonix desktop
2. Have multiple sessions with long conversations
3. Switch between tabs/sessions repeatedly
4. Over time (hours/days), system memory usage grows unbounded
5. OS OOM killer terminates Reasonix

## 预期行为 (Expected Behavior)

Detached sessions should be automatically garbage-collected after a timeout (e.g., 5 minutes of inactivity) or when the number of detached sessions exceeds a reasonable limit (e.g., keep at most 3-5). The `detachedSessions` map should have an LRU-style eviction policy.

## 实际行为 (Actual Behavior)

Detached sessions accumulate indefinitely, consuming memory until the OS OOM killer terminates the process.

## 建议修复 (Suggested Fix)

Add a periodic GC mechanism for `detachedSessions` — for example, close and remove sessions that have been detached for more than N minutes, or limit the total number of detached sessions with LRU eviction. Consider adding a configurable `max_detached_sessions` option.

## 版本信息 (Version)

Current HEAD: `698e39a9` (desktop-v1.17.7-210-g698e39a9)
Introduced in the commit range `07c65c22..698e39a9` (320 files changed, ~23K insertions)
