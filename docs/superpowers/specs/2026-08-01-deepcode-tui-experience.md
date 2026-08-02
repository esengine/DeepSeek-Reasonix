# DeepCode TUI 体验重构蓝图（v2，实验性大改）

- 日期： 2026-08-01
- 分支： `deepcode`
- 前置： `2026-08-01-deepcode-ui-design.md`（Ink & Signal 色彩体系已落地）
- 范围： `internal/cli/` 的渲染层全面重构。模型/更新逻辑不动（`Update`、事件流、控制器），只重写"画"的部分。

## 0. 设计目标

把 TUI 从"纯文本输出"升级为"有设计系统的终端应用"：品牌化的启动画面、容器化的输入区、真正的状态栏（band）、树形化的工具流、行动导向的审批卡。所有颜色取自 `activeCLITheme` 调色板（Ink & Signal），不新增游离 hex。

## 1. 启动横幅 —— 品牌时刻（shell agent）

现状：`◆ reasonix · label` + 一行 dim 提示。
新版：5 行渐变"火花"宝石标 + 右侧信息列：

```
   ▄▄      ◆ reasonix · v2.1.0
  ████     model deepseek-v4-pro · effort high
 ██████    ~/Repos/DeepSeek-Reasonix
  ████     / commands · ! shell · ? help
   ▀▀
```

- 左列 6 宽火花：行 0/4 用半块 `▄`/`▀` 出尖角，`gradientText` 纵向渐变（accent-strong → accent，truecolor；非 truecolor 退化为 accent 单色）。
- 右列：`◆` accent + `reasonix` bold + ` · v…` faint；model 行用 info 色 + faint；路径行 faint；提示行 faint。
- 宽度 < 46 时退化为上下堆叠（火花行居中可选，直接顺序排即可）。
- missing-key 警告保留在下方，warn 色，前缀 `! `。
- 实现：`renderTUIBanner` 重写；火花行由 `gradient.go` 的 `sparkMarkLines()` 提供。

## 2. 输入区 —— 容器化（shell agent）

现状：仅上下边线 + accent 色。
新版：完整圆角框（`lipgloss.RoundedBorder()`），状态化边框色：

- 常态：`border` 色（安静）
- turn 运行中：`accent` 色（"活着"的信号）
- shell 模式（`!` 前缀）：`success` 色（沿用现状语义，View 中已有 override 逻辑）

连带修正（必须全做，否则光标错位）：

- `View()` 光标偏移：`cur.X += 2`（左边框 + PaddingLeft(1)），`cur.Y` 逻辑不变（顶边框仍是 1 行）；`nativeScrollback` 分支同步。
- `composerCursor()` 若内部另有 X 假设，一并核对。
- 框总宽不变（`.Width(boxW)`），内部 textarea 可用宽少 2 列，相关 composer 渲染测试按新几何更新。

## 3. 状态栏 —— 扁平文字化（footer agent）

现状：2-3 行纯文本 + `─` 分隔线。
新版（灰色调扁平化）：无衬底状态带，纯 faint 文字行 + hairline 分隔：

- `statusBlockStyle` **去掉** `Background(userBubbleBG)`：状态区回到纯文字（flat），靠 hairline 与对话区轻分层。
- 模式标签（modeTag）为**扁平文字**：`● 模式名`，语义色前景 + bold（plan=accent / auto=warn / yolo=danger / shell=success），无胶囊背景。
- 两行信息之间用 `statusFooterDivider(width)` 渲染 hairline `─`（border 色），颜色层级：label subtle / value muted / metric 色不变。
- 自定义 statusline 契约不变（替换全部内建字段）。

## 4. 进度行 —— 更丰富（shell agent）

现状：`⠋ Thinking… 12s · ↓1.2k`。
新版：`⠋ Reasoning… 12s · 3 tools · ↓1.2k · esc interrupt`

- spinner 保持 braille + accent；动词用 accent，其余 faint。
- 若 `chatTUI` 有现成的本轮工具调用计数字段则显示 `N tools`，没有就不加（不许为此改 Update 逻辑）。
- 右侧 `esc interrupt` 提示（faint，宽不够时优先省略）。

## 5. 审批卡 —— 行动导向（shell agent）

现状：accent 框 + `⏸` + 编号选项。
新版：

- 新样式 `approvalPanelStyle`（地基在 theme.go 声明）：边框 `warn` 琥珀色 —— "需要动作"与 accent 的"品牌"语义分离。
- 标题行 `⏸` 用 warn 加粗；reason 等次要信息 faint。
- 选中行保持 rowLine 现状逻辑；底部快捷键提示改为 `y approve · a always · p plan · n nope · ↑/↓ · ⏎`（faint）。
- 问答题卡（chooser）继续用 accent 的 choicePanelStyle 不动。

## 6. Todo 面板 —— 进度感（shell agent）

- 标题行：`Tasks ▓▓▓░░░░░ 2/5` —— 10 格进度条，已完成格 `success` 色、未完成格 `border` 色；计数 faint。
- in_progress 行：`▶` 用 accent（替代 yellow），label 用 fg；completed 行 success `✔` + faint；pending 行 faint `○`。
- 面板顶边框色保持 `border`。

## 7. 工具卡 —— 树形两列式（stream agent）

现状：`● Verb(arg)`。
新版：`● Verb  arg`

- 去掉括号：verb bold，空两格，arg 用 `muted` 色直接跟随（按宽度 clamp）。
- 类别色点语义不变（read=toolRead 青 / write=success 绿 / exec=warn 琥珀 / proc=toolProc 紫 / 其他=accent）。
- `⎿` 延续槽不变。diff 块头（toolHead 复用处）同步新格式。

## 8. 用户气泡与思考块（shell agent）

- 用户气泡：`›` accent bold + 正文用普通 fg（整条橙色太吵）；plan 模式前缀 `› [plan]`（accent chip 文字）。NO_COLOR 路径保持 `│ › `。
- 思考块：头部 `✦ Reasoning`（faint）或折叠后 `✦ Thought for Ns`；导轨 `▎` 用 `border` 色（比 dim 更收敛）。

## 9. Markdown 微调（stream agent）

- ATX 标题：h1/h2 = bold + accent；h3 及以下 = bold fg。
- `---` 水平线：整宽 `─` 用 `border` 色。
- 代码块/引用导轨已按前序改造使用 border 色，保持。

## 10. 地基（我自己先实现，theme.go + gradient.go）

`refreshCLIStyles()` 变更 + 新声明（全部放 theme.go，避免与其他 agent 的文件冲突）：

```go
var approvalPanelStyle lipgloss.Style // warn 边框圆角? 否——NormalBorder 顶边，与 choicePanelStyle 同构
// refreshCLIStyles 内：
inputBoxStyle    = RoundedBorder 全四边, BorderForeground(border), PaddingLeft(1)
approvalPanelStyle = NormalBorder 顶边, BorderForeground(warn), PaddingLeft(1)
compSelStyle     = Background(accent).Foreground(accentFg).Bold(true)  // 反白选中
statusBlockStyle = themeStyle(faint).Background(themeLipColor(userBubbleBG))
```

`gradient.go`（新文件）：

```go
// gradientText 对 s 逐 rune 在 from→to 间做 RGB 插值（truecolor）；
// 非 truecolor 退化为 themeFg(from, s)。包含 " " 的行保持空格不着色。
func gradientText(s string, from, to cliColor) string
// sparkMarkLines 返回 5 行 × 6 列的火花宝石标（已按行应用纵向渐变）。
func sparkMarkLines() []string
```

测试契约更新（地基时一并做）：

- `TestComposerBorderAndCursorTrackThemeAccent` → 改为断言 idle 态 `inputBoxStyle` 边框 == `border` 色（重命名为 `TestComposerBorderTracksThemeBorder`，光标 accent 断言保留）。
- 若有 pin `compSelStyle` / `statusBlockStyle` 的测试，同步更新。

## 11. 实施分工（phase 2，三个并行 agent，均不得改 Update/事件逻辑）

| Agent | 文件 | 内容 |
|---|---|---|
| shell | `chat_tui.go`（独占）+ 相关测试 | §1 横幅、§2 输入区+光标数学、§4 进度行、§5 审批卡、§6 todo 面板、§8 用户气泡/思考块 |
| footer | `status_footer.go`、`gitstatus.go` + 相关测试 | §3 状态带、分隔线处理；修地基导致的状态测试失败 |
| stream | `toolcard.go`、`transcript.go`、`md.go` + 相关测试 | §7 工具卡、§9 markdown；修地基导致的工具/转写测试失败 |

约束：

- 不许新增游离 hex：一律 `activeCLITheme.*`。
- i18n：尽量复用现有 key；必须新增时 en/zh/zh-TW 三目录同步加（`TestCatalogsComplete` 会查）。
- 保留所有现有快捷键/交互行为；仅视觉与信息层级变化。
- `NO_COLOR` / 非 truecolor / 窄终端（<40）路径不得裸奔。

## 12. 验证

- `go build ./... && go vet ./internal/cli/`
- `go test ./internal/cli/ -count=1`：除既有环境性失败 `TestRunResumeCopyContinuesInDuplicate` 外全绿。
- 目测检查清单：banner 渐变、输入框三态边框、状态带、审批琥珀框、工具卡新格式、todo 进度条。
