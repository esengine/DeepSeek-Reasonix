# DeepCode — TUI & Web UI 世界级设计改造

- 日期： 2026-08-01
- 分支： `deepcode`
- 范围： CLI TUI（`internal/cli/`）与桌面端 Web UI（`desktop/frontend/`）
- 不在范围： `site/`（营销/文档站）、`workers/`、`docs/themes/`、官方主题包美术资产

## 1. 设计语言：「Ink & Signal」（深墨与信号）

概念：**深空中的一点思想火花**。开发者工具的世界级审美（Linear 的克制、Zed 的编辑器级锐利、Raycast 的精致）归结为五条纪律：

1. **完美的中性色阶梯** —— 冷调"深墨"蓝灰阶（暗色）/ 冷调"瓷白"阶（亮色），绝不使用纯黑纯灰。
2. **灰色调为主（Graphite-first）** —— 全产品以石墨灰为签名色；焦点、选中、光标与模式标签统一由中性冷灰蓝承担，语义色一律降饱和到灰系邻域（灰绿/灰琥珀/灰红），避免任何高饱和色块。品牌符号 `◆` 保留，但只以灰阶渐变呈现。
3. **语义色同族和谐** —— ok/warn/err/info 与签名色在相同明度/饱和度家族内（低饱和、贴近灰阶），不打架。
4. **扁平层级** —— 层级靠细线（hairline）与留白，不靠实心背景块；状态条、模式标签等一律扁平文字化，无胶囊背景、无 tinted band。
5. **动效快而短** —— 100–340ms，decelerate 曲线，绝不喧宾夺主。

## 2. 色彩规范（精确值）

### 2.1 暗色 —— "Deep Ink"（石墨灰为主）

| 角色 | 值 | 说明 |
|---|---|---|
| bg | `#0a0c11` | 应用基底，深墨微蓝 |
| bg-soft | `#0f1219` | 次层 |
| bg-elev | `#151a23` | 浮层（卡片、popover） |
| bg-elev-2 | `#1c2230` | 更高浮层（hover、按钮） |
| sidebar-bg | `#0c0f16` | 侧栏 |
| chat-bg | `#0b0d13` | 对话区 |
| border | `#2a2d35` | 主描边（hairline，低对比） |
| border-soft | `#1a202b` | 次级描边 |
| fg | `#eceff4` | 主文本（柔和白，非纯白） |
| fg-dim / muted | `#b8bcc6` | 次文本 |
| fg-faint | `#767b87` | 弱文本/占位 |
| subtle | `#8f95a1` | 标签/提示 |
| **accent (Graphite)** | `#9aa3b2` | 签名冷灰蓝：焦点、选中、光标、模式标签 |
| accent-strong | `#c3c9d4` | hover/渐变端（灰阶亮端） |
| accent-fg | `#1a1d23` | 签名色上的文字（近黑） |
| accent-soft | `rgba(154,163,178,0.14)` | 签名色薄涂 |
| 签名渐变 | `linear-gradient(135deg, #9aa3b2, #c3c9d4)` | 灰阶渐变，仅品牌时刻 |
| ok / success | `#8fbc98` | 灰绿 |
| warn | `#c9b383` | 灰琥珀 |
| err | `#cc8f8b` | 灰红 |
| danger | `#d9807a` | |
| info | `#8fabb8` | 灰蓝 |
| secondary | `#a9a1c4` | 灰紫 |
| diff add bg/fg | `rgba(143,188,152,0.12)` / `#9cc7a5` | |
| diff del bg/fg | `rgba(204,143,139,0.12)` / `#d6a29e` | |
| syntax (Graphite Dark) | keyword `#c678dd` · string `#98c379` · number `#d19a66` · comment `#5c6370` · func `#61afef` · type `#e5c07b` · builtin `#56b6c2` · meta `#848b96` | onedark 的低饱和灰系（chroma `onedark`） |

### 2.2 亮色 —— "Porcelain"

| 角色 | 值 |
|---|---|
| bg | `#eceff5` |
| bg-soft | `#f4f6fa` |
| bg-elev | `#ffffff` |
| bg-elev-2 | `#eef1f7` |
| sidebar-bg | `#f2f4f9` |
| chat-bg | `#f9fafc` |
| border | `#d6d8dd` |
| border-soft | `#e2e8f0` |
| fg | `#141a26` |
| fg-dim / muted | `#4d5159` |
| fg-faint | `#838792` |
| subtle | `#686d77` |
| **accent (Graphite)** | `#4a515c` |
| accent-strong | `#3a4049` |
| accent-fg | `#ffffff` |
| accent-soft | `rgba(74,81,92,0.11)` |
| ok / success | `#3f7a52` |
| warn | `#8a6f33` |
| err | `#a8524e` |
| info | `#3f7485` |
| secondary | `#6f6596` |
| diff add bg/fg | `rgba(63,122,82,0.09)` / `#3f7a52` |
| diff del bg/fg | `rgba(168,82,78,0.08)` / `#a8524e` |
| syntax (Ink Light) | keyword `#a626a4` · string `#50a14f` · number `#986801` · comment `#6a737d` · func `#4078f2` · type `#e45649` · builtin `#0184bc` · meta `#6a737d` |

### 2.3 形状 / 投影 / 动效

- 圆角阶梯：`--radius: 10px`（卡片），list/tree row `8px`，button `8px`，pill 全圆。
- 焦点环（双环，世界级细节）：`0 0 0 2px var(--bg), 0 0 0 4px color-mix(in srgb, var(--accent) 55%, transparent)`。
- 投影分层：shadow-1 `0 1px 2px rgba(0,0,0,.28), 0 1px 1px rgba(0,0,0,.18)`；shadow-2 `0 18px 48px rgba(0,0,0,.38), 0 4px 12px rgba(0,0,0,.24)`。
- 选中色：`::selection` = accent 30% 薄涂。
- 动效沿用现有 token（120/180/340/420ms + decelerate），不新增。

## 3. TUI 改造（`internal/cli/`）

### 3.1 调色板重写（`theme.go`）

`cliPalette` 全部替换为 §2 的值（xterm 256 手工回退一并更新）：

**dark（graphite 基底）**：accent `#9aa3b2`/247 · muted `#b8bcc6`/250 · faint `#767b87`/243 · subtle `#8f95a1`/245 · success `#8fbc98`/108 · warn `#c9b383`/179 · err `#cc8f8b`/174 · danger `#d9807a`/167 · info `#8fabb8`/109 · secondary `#a9a1c4`/146 · border `#2a2d35`/236 · selection=accent · userBubbleBG `#16181d`/234 · diffAddBG `#1a2e21`/22 · diffDelBG `#34211f`/52 · toolRead `#8fabb8`/109 · toolProc `#b3a8cc`/146

**light（sandstone 基底）**：accent `#4a515c`/240 · muted `#4d5159`/239 · faint `#838792`/244 · subtle `#686d77`/242 · success `#3f7a52`/65 · warn `#8a6f33`/137 · err `#a8524e`/131 · danger `#b04842`/167 · info `#3f7485`/30 · secondary `#6f6596`/104 · border `#d6d8dd`/253 · selection=accent · userBubbleBG `#e9eaed`/255 · diffAddBG `#e3efe6`/194 · diffDelBG `#f7e7e5`/255 · toolRead `#3f7485`/30 · toolProc `#7a6fa6`/97

**8 个主题风格（名称不变以免破坏用户配置）**：

- graphite `#9aa3b2`/247 "graphite gray accent"（暗色默认，灰色调签名）
- ember `#f5832e`/209 "hot ember accent"
- aurora `#3ecfae`/79 "cool teal accent"
- midnight `#b494f5`/141 "quiet violet accent"
- sandstone `#4a515c`/240 "graphite gray accent"（亮色默认，灰色调签名）
- porcelain `#7c5cc8`/104 "soft violet light accent"
- linen `#c25544`/167 "muted coral light accent"
- glacier `#2e7fa8`/67 "cool blue light accent"

只有 graphite/sandstone（默认）走灰色 accent；其余 6 个风格是用户显式 `/theme` 选择的个性 accent，中性色一律继承灰调基底。

### 3.2 消灭调色板之外的硬编码

- `gitstatus.go` 模式色：plan 模式用**签名 accent**（石墨灰，与产品基调一致）；auto 保持灰琥珀 `#c9b383`（暗）/ `#8a6f33`（亮）；yolo=danger；shell=success。modeTag 现在是**扁平文字标签**：`● 模式名`，语义色前景 + bold，无实心胶囊背景。实现方式：在 `refreshCLIStyles()` 中由 `activeCLITheme` 派生，不再使用包级常量。
- `diffview.go` 的 `bgDiffAdd/bgDiffDel/fgDiffAdd/fgDiffDel` 四个 SGR 常量改为从 `activeCLITheme.diffAddBG/diffDelBG` 经现有 `fgSGR/bgSGR` 派生；chroma 风格按主题明暗选择 `onedark`（暗，低饱和灰系）/ `github`（亮）。

### 3.3 Markdown 代码块语法高亮（`md.go`）—— 最大的体验升级

现状：围栏代码块无高亮，只有 accent 文本 + dim `│` 导轨。chroma 已是依赖（diffview 在用）。

- 用 chroma（`lexers.Match`/`Analyse` 回退 `fallback`）+ `terminal256` formatter 渲染围栏代码块内容；风格按主题明暗选 `onedark`/`github`（暗色用 onedark 保持灰调，不用 github-dark 的高饱和）。
- 无语言标注或词法器缺失时保持现状渲染路径（accent/dim），保证不回归。
- 保留 `│ ` 导轨，但导轨色改用 `border` 色（比 dim 更收敛）。
- 注意终端宽度换行：高亮仅作用于 token 着色，不改变现有的宽度处理/软换行逻辑。

### 3.4 扁平化细节打磨

- 横幅 `◆ reasonix · label`：`◆` 用 accent（灰蓝）、名称加粗用主文本色、`· label` 用 faint；spark 渐变改为灰阶（warn 灰琥珀 → accent 灰蓝）。
- `refreshCLIStyles()`：inputBox/choicePanel/todoPanel 顶部描边用 `border` 灰；accent 只留给焦点/选中/光标。
- **状态条扁平化**：`statusBlockStyle` 去掉 tinted band（userBubbleBG 背景），改为纯 faint 文字；两行信息之间用 `statusFooterDivider` 渲染 hairline `─`（border 色）分隔。
- **模式标签扁平化**：modeTag 从实心彩色胶囊改为 `● 模式名` 文字（语义色前景 + bold），语义完全由颜色承载。
- `boxed()`（doctor/welcome 框）边框从 accent 改为 border 灰。

## 4. Web UI 改造（`desktop/frontend/`）

### 4.1 令牌层重写（核心，改动即全局）

按 §2 重写 `src/styles.css` 三处令牌块（必须三处同步）：

1. 基础 `:root`（约 20–228 行）：surfaces/text/brand+semantic/diff/syntax/shadow/focus-ring/radius。
2. 两块亮色（`@media (prefers-color-scheme: light)` 与 `:root[data-theme="light"]`，约 305–442 行）：两块的值必须保持一致。
3. "Native Workbench refresh" 层（约 25222–25288 行）：同样按 §2 的新值重写（它靠源码顺序覆盖基础层）。

同时更新 `src/lib/theme.ts` 的 `syncNativeWindowBackground`：暗色 `(10, 12, 17)`（#0a0c11），亮色 `(236, 239, 245)`（#eceff5）。

### 4.2 主题方向（directions）

- graphite（默认方向）完整按 §2 更新 accent 族值（`#d97757→#ec7048`、`rgba(217,119,87,*)→rgba(236,112,72,*)`、`#e58a6b/#e38b6b→#f4906c`、accent-fg→`#1a0d06`）。
- 其余 5 个方向（aurora/slate/carbon/nocturne/amber）本轮**不动**：它们是个性化备选主题，中性色会自动继承基础层新值；默认面貌 = graphite，集中火力打磨。

### 4.3 高杠杆组件精修（在令牌之外，控制在小范围）

- 焦点环：全应用换上 §2.3 双环（改 `--focus-ring` 令牌即可）。
- `::selection`：若无定义则新增 accent 30% 薄涂。
- 按钮：暗色下主按钮用签名渐变背景（§2.1 渐变）+ accent-fg 文字；次按钮增加极弱顶部内高光（`inset 0 1px 0 rgba(255,255,255,0.05)`，仅通过修改现有 button 规则，不新增选择器）。
- 圆角令牌按 §2.3 调整（`--radius` 9→10、button 7/8→8、list/tree row 7→8）。

### 4.4 硬约束（违反会挂构建/测试，必须处理）

- z-index 只能 `var(--z-*)`；不新增 z-index。
- Bundle 预算：initial CSS gzip ≤ 112 KiB —— 本次以"改值"为主，净增规则数控制在 ~10 条以内。
- 契约测试需同步更新（仅更新被钉住的视觉值，不改测试结构）：
  - `__tests__/theme-auto-background.test.ts`：钉住 workbench 亮色 `--bg:#e9edf3`/`--fg:#121722`/`--workspace-files-bg:#f1f4f8` 及 theme.ts 的 RGB 元组 → 更新为新值。
  - `__tests__/theme-pack.test.ts`、`bundle-contract.test.ts`、`layout-style-defaults.test.ts` 等：若断言到被改的旧 hex/圆角值，同步更新。
- 平台约束：禁用 backdrop-filter；颜色一律走 CSS 变量；`--wails-draggable` 区域不动。

## 5. 验证

- TUI：`go build ./...` + `go test ./internal/cli/...`（全绿）。
- Web：`cd desktop/frontend && pnpm build`（含 css 语法/z-index/tsc/bundle 预算）+ `pnpm test` 中与视觉契约相关的测试文件全绿。
- 对照检查：改动后 grep 确认 `styles.css` 三处令牌块无残留旧值（`#090a0c`、`#d97757`、`#ec7048`、`#e9edf3` 等）。

## 6. 成功标准

- 暗色默认主题第一眼质感显著提升：更深邃的墨底、更收敛的 hairline 描边、以石墨灰为主基调的扁平界面（无实心胶囊、无 tinted band）。
- 亮色模式同样精致（瓷白 + 石墨），不是暗色的附属品。
- TUI 代码块拥有低饱和（onedark）语法高亮；TUI 与 Web 共享同一套灰调色彩语言（同 hex 家族）。
- 所有现有测试与构建守卫保持绿色（视觉契约测试按新设计更新）。
