# TUI 自动滚动偶尔失效的现象记录

> 记录日期：2025-07  
> 状态：未确认的观察，不一定是 bug

## 现象

Agent 回复时，TUI 的 transcripts 视口偶尔不再自动跟随到最新内容，用户需要手动滚动才能看到刚输出的新行。通常在底部面板（如 `/todo`、Ctrl+O 展开推理内容、approval banner 等）切换时出现。

## 现有机制

核心代码在 `internal/cli/chat_tui.go:625-658`（`Update` 外层包装器）：

```
1. wasAtBottom = m.viewport.AtBottom()     // 拍快照
2. m.update(msg)                            // 处理事件
3. 条件判断：len(transcript) 变了 / width 变了 / transcriptDirty
4. 若 wasAtBottom == true: GotoBottom()    // 跟随
```

详见 `internal/cli/chat_tui_explanation.md` 或阅读源码的 `TestTranscriptTailFollow` 测试用例。

## 三种推测

### 推测 A：视口高度变化后 yOffset 快照过期（P0 — 最可能）

**时序问题**：每次 `Update` 都执行 `SetHeight(transcriptHeight())`（L639），无论 transcript 是否变化。当底部面板开/闭时：

1. `transcriptHeight()` 改变 → `SetHeight` 改变视口高度
2. `maxYOffset = max(0, totalContentLines - height)` 随之改变
3. 但 `yOffset` 没变，所以 `AtBottom()` 可能从 true 变为 false（如果旧 yOffset < 新 maxYOffset）
4. **问题**：`wasAtBottom` 是在 `SetHeight` **之前** 捕获的快照（L626），它反映的是旧状态
5. **触发条件**：这次 `Update` 如果 transcript 内容没变也没 dirty（纯粹的高度变化）→ 条件 L642 为 false → `SetContent` 和 `GotoBottom` 都不执行
6. **结果**：视口停在旧 yOffset，但真正的底部已经移动了。用户看到内容"缩在"视口上半截

**面板关闭时逆向同样成立**：高度变大 → `maxYOffset` 变小 → `yOffset` 可能 > `maxYOffset`（`PastBottom`），但 `AtBottom()` 仍返回 true → 下次内容更新时 `GotoBottom` 往**上**跳一截。

### 推测 B：就地替换行的包裹行数变化

`streamAnswer()`（L1776）在一次 agent 回复中多次发生：首次 `commitLine`（追加），后续 `transcript[m.answerIdx] = block` + `transcriptDirty = true`（就地替换）。

当旧 block 包裹为 X 行、新 block 包裹为 Y 行时：

- `len(transcript)` 不变，`transcriptDirty` = true → `SetContent` 执行
- `wasAtBottom == true` → `GotoBottom` 能纠正到正确底部
- `wasAtBottom == false` → 旧 yOffset 指向新包裹文本的不同逻辑位置，视口在用户看来偏移了

### 推测 C：批量事件处理中等效内容与面板交替

`agentEventMsg` 的 drain 循环（L1134-1148）最多一次消费 512 个事件，但只执行一次 `wasAtBottom` 快照、一次 `SetContent`、一次 `GotoBottom`。如果这批事件中既有面板状态变化又有内容追加，最终快照反映的是最早的状态而不是叠加后的状态。

## 复现尝试思路

1. 有底部面板打开（如 `/todo`），观察新内容到来时光标是否在底部
2. 滚动上去阅读历史，看新内容是否意外地"拽"回底部（说明 gating 有问题）
3. 快速切换底部面板（多次 Ctrl+O 展开/折叠推理），观察 viewport 是否"卡住"
4. 打开底部面板 → agent 回复新内容 → 关闭底部面板，观察视口位置是否正确

## 参考

- `internal/cli/chat_tui.go:625-658` — Update 外层包装器
- `internal/cli/chat_tui.go:1406-1413` — transcriptHeight()
- `/viewport/viewport.go:189-191` — AtBottom()
- `/viewport/viewport.go:301-306` — maxYOffset()
- `internal/cli/chat_tui_test.go:1009-1036` — TestTranscriptTailFollow
