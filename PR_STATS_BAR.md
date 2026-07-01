# PR: Real-time Token Statistics Floating Bar / 实时 Token 统计悬浮窗

> **Branch:** `feature/stats-bar`
> **Base:** `main-v2`
> **Author:** Reasonix Admin

---

## Overview / 概述

This PR adds a real-time token statistics display to the desktop chat interface, showing input tokens (cache miss), cache hit tokens, output tokens, tokens-per-second, and elapsed time — both during streaming (in the Composer stop button area) and after completion (alongside the fork/summary/rewind action buttons). Stats are persisted across session switches and application restarts.

本 PR 为桌面客户端聊天界面增加了实时 Token 统计显示功能：输入 Token（未命中缓存）、缓存命中 Token、输出 Token、生成速度（tok/s）和耗时。流式过程中显示在 Composer 停止按钮区域，对话完成后显示在分叉/总结/回溯按钮旁。统计数据跨会话切换和应用重启持久化。

---

## Changes Summary / 变更总览

| File / 文件 | Type / 类型 | Description / 描述 |
|------------|-------------|-------------------|
| `desktop/frontend/src/components/StatsBar.tsx` | **NEW** | `LiveStatsContext` provider for streaming stats data |
| `desktop/frontend/src/components/Message.tsx` | MODIFIED | TurnActions stats box, timestamp format, assistant timestamps |
| `desktop/frontend/src/components/Transcript.tsx` | MODIFIED | Track usage/userCreatedAt, pass to TurnActions |
| `desktop/frontend/src/components/Composer.tsx` | MODIFIED | Real-time stats in stop button area |
| `desktop/frontend/src/App.tsx` | MODIFIED | Pass accumulated state to Composer/Transcript |
| `desktop/frontend/src/styles.css` | MODIFIED | Styles for stats components |
| `desktop/frontend/src/lib/useController.ts` | MODIFIED | Global accumulators, item types, event handling |
| `desktop/frontend/src/lib/types.ts` | MODIFIED | `HistoryMessage.usage` field |
| `internal/provider/provider.go` | MODIFIED | `Message.Usage` field, `Usage` JSON tags, `Message.CreatedAt` |
| `internal/agent/agent.go` | MODIFIED | Pass `Usage` + `CreatedAt` on `session.Add` |
| `desktop/app.go` | MODIFIED | `HistoryMessage.Usage` + `CreatedAt`, load-time copying |

---

## Detailed Changes / 详细变更

### 1. StatsBar.tsx (New File / 新文件)

**Purpose / 用途:** Provides `LiveStatsContext` which carries streaming state (usage, turnStartAt, running) to child components. Used by `Transcript.tsx` to share real-time usage data with the hot zone.

**Exports / 导出:**
- `LiveStats` — Interface with `usage?: WireUsage`, `turnStartAt?: number`, `running: boolean`
- `LiveStatsContext` — React context for consuming live stats data

---

### 2. Message.tsx — TurnActions Stats Box / 对话操作栏统计框

**Purpose / 用途:** Adds a token statistics box to the right of the fork/summary/rewind action buttons after a turn completes.

**New functions / 新函数:**
- `fmtTurnToken(n)` — Format token count (1.2K, 3.4M, etc.)
- `fmtTurnElapsed(userCreatedAt)` — Compute elapsed seconds from user message timestamp (`Date.now() - userCreatedAt`)
- `fmtTurnTps(tokens, userCreatedAt)` — Compute real tokens-per-second from output tokens and elapsed time

**Changes / 改动:**
- Added `usage` and `userCreatedAt` props to `TurnActions`
- Replaced fake TPS (`completionTokens / 5`) with real computation from timestamps
- Changed `formatMessageTime` from `HH:MM` to `HH:MM:SS`
- Added timestamp display to `AssistantMessage` (via `item.createdAt`)

---

### 3. Composer.tsx — Stop Button Area Stats / 停止按钮区域统计

**Purpose / 用途:** Displays real-time token statistics while the model is generating.

**Changes / 改动:**
- Added `turnCacheHitTokens` and `turnCacheMissTokens` props
- Display input miss (`cacheMissTokens`) instead of total input (`promptTokens`)
- TPS updates every second via `useTick(running)`

---

### 4. useController.ts — State Accumulators / 状态累加器

**Purpose / 用途:** Core state management for per-turn and global token accumulation.

**New state fields / 新增状态字段:**

| Field / 字段 | Description / 说明 |
|-------------|-------------------|
| `turnPromptTokens` | Per-turn accumulated prompt tokens (deprecated, replaced by global deltas) |
| `turnCacheHitTokens` | Per-turn accumulated cache hit tokens |
| `turnCacheMissTokens` | Per-turn accumulated cache miss tokens |
| `globalTokens` | Total tokens across entire session (never reset) |
| `globalCacheHitTokens` | Total cache hit tokens across entire session |
| `globalCacheMissTokens` | Total cache miss tokens across entire session |
| `turnStartGlobalTokens` | Snapshot of `globalTokens` at turn start (for computing turn delta) |
| `turnStartGlobalCacheHit` | Snapshot of `globalCacheHitTokens` at turn start |
| `turnStartGlobalCacheMiss` | Snapshot of `globalCacheMissTokens` at turn start |

**Key event handlers / 关键事件处理:**

| Event / 事件 | Logic / 逻辑 |
|-------------|-------------|
| `turn_started` | Snapshot global accumulators, reset per-turn accumulators; update previous turn's item with final delta |
| `usage` | Update global accumulators from `e.usage`; set `item.usage` with accumulated values |
| `turn_done` | Update last assistant item with final turn delta from global accumulators (catches background calls) |
| `message` | Set `item.usage` with accumulated turn delta |

**Item types / 消息类型:**
- `assistant` item: added `createdAt?: number` and `usage?: WireUsage`

**History loading / 历史加载:**
- `historyMessagesToItems`: copies `m.usage` to assistant items for persistence

---

### 5. provider.go & agent.go — Go Backend Persistence / Go 后端持久化

**provider.go:**
- `Message.Usage *Usage` — Store per-message token telemetry
- `Usage` struct: added `json:` tags (`promptTokens`, `completionTokens`, etc.) matching frontend `WireUsage`
- `Message.CreatedAt int64` — Unix millisecond timestamp for persistence

**agent.go:**
- `session.Add(provider.Message{..., Usage: usage, CreatedAt: time.Now().UnixMilli()})` for:
  - User messages
  - Assistant messages (main path)
  - Assistant messages (error recovery path)

---

### 6. app.go — History Conversion / 历史消息转换

**HistoryMessage struct:**
- Added `Usage *provider.Usage` — persisted per-turn token data
- Added `CreatedAt int64` — per-message timestamp

**Key functions / 关键函数:**

| Function / 函数 | Change / 改动 |
|----------------|--------------|
| `historyMessagesWithPlannerDisplaysAndLookups` | Copies `m.Usage` and `m.CreatedAt` to `HistoryMessage` |
| `previewSessionMessages` | Falls back from event-format to provider.Message format, preserving usage |
| `previewEventSessionMessages` | Captures `usage` events, attaches to preceding `model.final` |

---

### 7. Transcript.tsx — Usage/UserCreatedAt Capture / 数据捕获

**Purpose / 用途:** Captures `item.usage` and `item.createdAt` from assistant and user items respectively, passing them to `TurnActions` for display.

**Changes / 改动:**
- Hot zone `useMemo`: tracks `actionUsage`, `actionUserCreatedAt`
- `WarmTurnItems`: tracks `actionUsage`, `actionUserCreatedAt`
- Both pass `usage` and `userCreatedAt` as props to `TurnActions`
- Provides `LiveStatsContext` for streaming data

---

### 8. types.ts — TypeScript Types / 类型定义

```ts
export interface HistoryMessage {
  // ... existing fields ...
  usage?: WireUsage;  // NEW: persisted per-turn token telemetry
}
```

---

### 9. styles.css — Component Styles / 组件样式

**New styles / 新样式:**

| Selector / 选择器 | Purpose / 用途 |
|------------------|---------------|
| `.turn-actions__stats` | Stats box in TurnActions area |
| `.turn-actions__stats-icon` | Icon sizing for stats |
| `.composer-runstatus__stat` | Stat indicator in stop button area |
| `.composer-runstatus__stat-icon` | Icon sizing for stop area stats |
| `.msg--assistant` | Added `position: relative` for stats bar anchoring |

---

## Key Design Decisions / 关键设计决策

### Why global accumulators + turn-start snapshots? / 为什么使用全局累加器 + 轮次快照？

Using global accumulators (never reset) with turn-start snapshots ensures:
- Background API calls (e.g., title generation) that happen after `turn_done` are still counted in the session total
- Per-turn deltas are computed at the START of the next turn, catching all events including post-turn processes
- The `turn_done` handler also computes the delta for the last turn (no subsequent `turn_started`)

### How is persistence achieved? / 如何实现持久化？

1. `agent.go` stores `Usage` + `CreatedAt` in `provider.Message` via `session.Add()`
2. `agent.Session.Save()` writes messages as JSONL to `.jsonl` files
3. `agent.LoadSession()` reads them back
4. `historyMessagesWithPlannerDisplaysAndLookups` copies `Usage` + `CreatedAt` to `HistoryMessage`
5. Frontend `historyMessagesToItems` creates items with `usage` + `createdAt`

### Why `cacheMissTokens` instead of `promptTokens` for input display? / 为什么输入显示用 cacheMissTokens 而非 promptTokens？

`promptTokens` = total input (cache hit + cache miss). Since cache hit tokens are displayed separately, showing total input would double-count. `cacheMissTokens` represents the actual uncached input that was charged.

---

## Testing / 测试

- TypeScript: `pnpm run typecheck` — ✅ zero errors
- CSS: `pnpm run check:css` — ✅ syntax + z-index tokens passed
- Go: `go build ./...` — ✅ compiles
- Full build: `wails build` — ✅ production build succeeded

---

## PR Link / PR 链接

https://github.com/SauronSkywalker/DeepSeek-Reasonix/pull/new/feature/stats-bar
