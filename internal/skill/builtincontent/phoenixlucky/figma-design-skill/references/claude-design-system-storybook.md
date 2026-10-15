# 次选蓝本：Claude 桌面端 UI 细节（第三方逆向「CDS Storybook」）

这不是 Figma 文件，是一份**第三方逆向**做的单文件 SPA——取法与 Figma 完全不同（见下）。

**来源属性：社区逆向，不是官方。** 几何 / 布局 / 动效实现层的数值取自源码字面量，可当重建参考用；但**不要当成 Claude 官方规格引用**——页面里的 `Verified from Claude.app` / `100% Fidelity` / `AST-Extracted` 都是作者写的徽标文案，不是证据。与官方 UI kit（`anthropic-mcp-apps-design.md`）冲突时，一律采用官方。

## 来源

| 项 | 值 |
|---|---|
| URL | https://claude-storybook.bystanderss.chatgpt.site/ |
| 页面标题 | `Claude Design System (CDS) - Storybook` |
| 自述版本 | `Claude Desktop v1.0` |
| 页面自称 | canvas 徽标 `Verified from Claude.app`；footer `REA Extracted Tokens` / `100% Fidelity`；Spring Lab 徽标 `AST-Extracted` |
| 技术栈 | Tailwind CDN + React 18 UMD + Babel Standalone（**内联 `<script type="text/babel">`，全部内容在一个 HTML 里**） |
| 提取日期 | 2026-10-14（复核） |
| 提取方式 | 下载当前 HTML 读取内联 CSS / JSX，并用浏览器 DOM 核对 17 个页面的目录与展示内容（不依赖截图识字） |
| 规模 | 3 大类 / 17 个 story：FOUNDATIONS 7 + COMPONENTS 7 + MOTION LAB 3 |

## 提取方法：源码即权威（与 Figma 相反）

Figma 的画布只能截图、变量面板才能读 DOM；**这类站点反过来——所有数值都以字面量写在 JSX 里，读源码比读 DOM 更权威、更快**。

```powershell
# 一次拿全（注意按 UTF-8 落盘，否则中文全变 mojibake）
$wc = [System.Net.WebClient]::new(); $wc.Encoding = [System.Text.Encoding]::UTF8
$wc.DownloadFile("<url>", "$env:TEMP\site.html")
```

定位三处就够：

| 位置 | 内容 |
|---|---|
| `<script>` 里的 `tailwind.config` | 颜色 / 字体 token 名 |
| `<style>` | `@keyframes` 全集、`.align-*` / `.text-wrap` 等实用类 |
| `<script type="text/babel">` 顶部的 `const STORIES = [...]` | 目录树；`const X = [...]` 数组就是每张 token 表 |

**反面提醒**：这个站点的"资料层"（token 名、数值、结论文案）全部是 JSX 里的字符串常量，DOM `innerText` 读到的只是渲染结果，反而更少。

---

## Foundations

### 1. Color

**4 层表面明度阶梯**（源自 `ColorsStory` 的 `surfaces` 数组）

| 层 | 用途 | Light | Dark |
|---|---|---|---|
| Surface 0 | 底基层 / 侧边栏 | `#f5f5f3` | `#0b0b0a` |
| Surface 1 | 主画布背景 | `#fcfcfb` | `#151515` |
| Surface 2 | 卡片 / 面板 | `#ffffff` | `#1a1a19` |
| Surface 3 | 输入框 / 最高层 | `#ffffff` | `#20201e` |

**陶土品牌色**（`clayColors`）

| 名 | 值 | 用途 |
|---|---|---|
| Clay | `#d97757` | 思考圆点、强调按钮、品牌图标 |
| Clay Emphasized | `#cc5a36` | 按钮悬停态、加重点击态 |
| Clay Light | `#fbf3ef` | 浅色选中态底衬 |

**暖灰中性阶**（`grays`，**不分明暗两套**）
`0 #ffffff` · `10 #fcfcfb` · `20 #f5f5f3` · `100 #dedcd6` · `300 #a4a19b` · `500 #6b6863` · `700 #373634` · `800 #20201e` · `850 #151515` · `900 #0b0b0a`

Tailwind 里注册成 `clay.{DEFAULT,hover,light}` 与 `surface.{light0..light3,dark0..dark3}`。
选择色：`selection:bg-[#d97757]/30`。站点自身外壳另用 `#0f0f0f`(暗色画布) / `#121212` / `#181818`(侧栏) / `#1e1e1e`(工具栏)——**这几个是 Storybook 自己的壳，不是 CDS 规格**。

### 2. Border Radius（`RadiusStory`）

站点声称统一缩放因子 `--cds-rem-scale: calc(16/17) ≈ 0.941`（见"取值注意"节：该变量在页面里**并未定义**）。

| 档 | 名义值 | 实测区间 | 用途 |
|---|---|---|---|
| XS | 4.7px | 4.7 ~ 5px | 标签、复选框、小按键 |
| SM | 5.6px | 5.6 ~ 6px | 小型按钮、Kbd 徽章 |
| MD | 8px | 7.5 ~ 8px | 标准按钮、下拉菜单项 |
| LG | 10px | 9.4 ~ 10px | 折叠卡片、Popover、附件卡片 |
| XL | 14px | 13.2 ~ 14px | 移动端输入框、内容模态框 |
| 2XL | **固定 20px** | 20px | Claude 桌面端 Composer 外壳 |
| Full | 999px | 999px | 分段胶囊、头像、状态徽章 |

Tailwind 映射写作 `rounded` / `-md` / `-lg` / `-xl` / `-2xl` / `-[20px]` / `-full`。

### 3. Heights & Sizing（`HeightsStory`）

| 档 | 高 | 用途 |
|---|---|---|
| XS | 20 ~ 24px (`h-6`) | 行内操作、标签关闭 |
| SM | 24 ~ 28px (`h-7`) | 密集工具条按钮 |
| **MD（默认）** | **32px = 2rem** (`h-8`) | 发送按钮、标准按钮、Tab |
| LG | 40px = 2.5rem (`h-10`) | 登录 / 主 CTA |
| Titlebar | 46 ~ 48px (`h-12`) | macOS 无边框拖拽区 |

### 4. Shadows（`ShadowsStory`）

**Composer 双环**（内外两环而非单一边框）

```
常态     0 0.25rem 1.25rem rgba(0,0,0,0.035), inset 0 0 0 1px rgba(0,0,0,0.08)
聚焦态   0 0.25rem 1.25rem rgba(0,0,0,0.075), inset 0 0 0 1px rgba(0,0,0,0.16), 0 0 0 1px rgba(0,0,0,0.16)
```

**通用阶梯**

| 档 | Light | Dark |
|---|---|---|
| SM | `0 1px 2px rgba(0,0,0,.05), 0 2px 8px rgba(0,0,0,.04)` | `0 1px 2px rgba(0,0,0,.3)` |
| MD | `0 2px 4px rgba(0,0,0,.06), 0 6px 16px rgba(0,0,0,.07)` | `0 4px 6px -1px rgba(0,0,0,.4)` |
| POPOVER | `0 8px 24px rgba(0,0,0,.12), 0 2px 6px rgba(0,0,0,.08)` | `0 8px 24px rgba(0,0,0,.5)` |

### 5. Typography & Align（`TypographyStory`，6 条自称"Constraint"）

| # | 约束 | 具体值 |
|---|---|---|
| 01 | 暗色光学字重补偿 | 浅色 `wght 500 / 600` → 暗色 `wght 460 / 560`，用 `font-variation-settings` 切换。话术依据是"视网膜光渗使白字显粗 10%~15%" |
| 02 | 行内图标光学基线 | 正文/标题 `.align-[-0.125em]`；11px caption 配 `.align-[-0.075em]`（页面 CSS 里另有 `-1px` / `-2px` 两个备用类）。理由：默认 baseline 让图标上浮 2~3px，`vertical-align:middle` 又会随行高漂移 |
| 03 | 智能换行 | 标题 `text-wrap: balance`；正文 `text-wrap: pretty`（杜绝孤字） |
| 04 | 长标识符折叠 | `overflow-wrap: anywhere; word-break: normal`（对比：`break-all` 会拦腰截断单词） |
| 05 | 等宽数字 | `font-variant-numeric: tabular-nums`，用于思考计时器、Token 计数 |
| 06 | 多行截断阶梯 | 侧栏标题 1 行 / 思考摘要 2 行 (`line-clamp-2`) / 系统提示词卡 3 行 (`line-clamp-3`) |

**实测正文字号**（散落在组件里，比 token 表更可信）：输入框 `text-[15px] leading-[1.4]`；AI 消息正文 `text-[15px] leading-[1.65]`；用户气泡 `text-sm`。
站点自己的字体栈是 `Inter` / `Newsreader` / `JetBrains Mono`——**不是 Anthropic Sans**（见"取值注意"节）。

### 6. Icons（`IconsStory`）

- 图标集：**Phosphor**，`viewBox="0 0 256 256"`，站点注明"提取自 `vendor-icons-DDsFzYXd.js`"
- 品牌火花 ✦ 是自定义路径，`viewBox="0 0 100 100"`
- **尺寸阶梯**：`xs 12` · `sm 14` · `md 16` · `lg 20` · `xl 24`
- **字重**：常规档写作 `533`（"原生标准字重 533.3"）；加粗档**不是换字形**，而是在同一 path 上叠 `stroke=currentColor strokeWidth=10 strokeLinecap=round`
- **Duotone**：叠第二条同 path，`opacity 0.28`、`transform="scale(0.96) translate(5,5)"`
- 收录 23 个：`spark chat fileCode fileText search copy check plus arrowRight chevronDown settings refresh trash stop info terminal branch cpu layers code eye pin user`

### 7. Motion Tokens（`MotionTokensStory`）

**时长** `--cds-dur-fast 60ms` · `-snap 120ms` · `-base 200ms` · `-sheet 300ms` · `-slow 450ms`

**贝塞尔** `--cds-ease-out cubic-bezier(.165,.84,.44,1)`（≈easeOutQuart）· `--cds-ease-snap cubic-bezier(.32,.72,0,1)` · `--cds-ease-overshoot cubic-bezier(.34,1.3,.64,1)`

**但真正的曲线全集要看 `<style>` 与各组件内联值**——下面这些token 表里没有、只存在于实现里：

| 曲线 | 用在 |
|---|---|
| `cubic-bezier(.215,.61,.355,1)` 90ms | `_grapheme-reveal` 逐字消解 |
| `cubic-bezier(.22,1,.36,1)` | `timeline-block-enter` 内部缓动 |
| `cubic-bezier(.4,0,.2,1)` 1.9s infinite | `timeline-quiet-dot` 思考三点 |
| `cubic-bezier(.19,1,.22,1)` 200ms | Thinking 折叠 `grid-template-rows 0fr↔1fr` |
| `cubic-bezier(.34,1.3,.64,1)` 0.45s | 用户气泡入场 `code-user-bubble-enter` |
| `cubic-bezier(.34,1.56,.64,1)` 1.1s | AI 消息 `message-fade-up` ⚠️ 见下注 |
| `cubic-bezier(.165,.84,.44,1)` 0.25s | 附件缩略图 `file-thumbnail-grow-in` |
| `cubic-bezier(.32,.72,0,1)` 150ms | 分段滑块位移 |

**关键帧全集**（`<style>` 里共 **11** 个，逐条点数与清单一致）

```
_grapheme-reveal_1plog_1   0% {opacity:0; filter:blur(6px)} → 100% {opacity:1; filter:blur(0)}
timeline-quiet-dot         0%,8% {opacity:0; translateY(2px)} · 22% {opacity:1; translateY(-2.5px)}
                           36%,70% {opacity:1; translateY(0)} · 90%,100% {opacity:0; translateY(0)}
timeline-block-enter       0% {opacity:0; blur(4px); translateY(5px) scale(.9)} · 30% {opacity:0; blur(4px); none}
                           → 100% {opacity:1; filter:none}
message-fade-up            0% {opacity:0; translateY(10px)} → 100% {opacity:1; translateY(0)}
code-user-bubble-enter     0% {opacity:0; scale(.92) translateY(6px)} → 100% {opacity:1; none}
byline-sent-bump           50% {scale(1.05)}
cds-skeleton-sweep         0% {translateX(-100%)} → 100% {translateX(100%)}
file-thumbnail-grow-in     0% {opacity:0; max-width:0; scale(.8)} → 100% {opacity:1; max-width:8rem; scale(1)}
file-thumbnail-upload-pulse 50% {opacity:.45}
saved-check-decay          to {opacity:0}
click-ring                 0% {opacity:.95; scale(.22)} → 100% {opacity:0; scale(1)}
```

> 两处口径修正（本文以源码为准）：
> 1. **计数**：清单共 **11** 个 keyframes。旧稿写"9 个"，与清单不符——这类"总数与清单不一致"是二手资料的常见特征，引用前先点数一遍。
> 2. **曲线参数**：`message-fade-up` 在 `<style>` 摘录里写成三参数的 `cubic-bezier(.34,1.56,1)`（CSS 非法，疑为摘录掉字），组件表里是四参数的 `.34,1.56,.64,1`。上面曲线表已统一为后者，与组件实现一致。

---

## Components

| 组件 | 实测规格 |
|---|---|
| **Chat Composer** | 外壳 `max-w-[42rem]`(672px) · `rounded-[20px]` · `p-[0.875rem]`(14px) · `gap-y-2` · 双环阴影；textarea `min-h-[3.25rem] max-h-[24rem]` · `text-[15px] leading-[1.4]`；placeholder `How can I help you today?`；左下 `Add content` + 模型选择器（都 `h-8`）；右下发送键 `size-8 rounded-lg` 陶土色，空文本时 `opacity-20` 禁用 |
| **Message Bubbles** | 用户气泡 `rounded-[18px] px-4 py-2.5 text-sm max-w-[85%]`，底色 Light `#f0f4f8` / Dark `white/10`，入场 0.45s overshoot；AI 消息无气泡，`text-[15px] leading-[1.65]`，带 spark + "Claude" 署名，入场 1.1s `cubic-bezier(.34,1.56,.64,1)` |
| **Thinking Accordion** | 三颗 **3px** 圆点、`gap-[3.5px]`、陶土色、`timeline-quiet-dot` 1.9s infinite，延迟 `0 / .15s / .3s`；完成后换成灰点 + `Thought for 3 seconds`；展开用 `grid-template-rows: 0fr → 1fr` 200ms，字重由 `chevronDown` 旋转 180° 配合 |
| **Buttons** | 尺寸 `XS 24px(h-6)` / `SM 28px(h-7)` / `MD 32px(h-8, 默认)` / `LG 40px(h-10)`；变体 Primary（clay 底 + `active:scale-95`）/ Secondary（1px 边 + 白底）/ Ghost（透明）/ Icon（`size-8`） |
| **Segmented Control** | 轨道 `h-9`(36px) · `p-[2px]` · `rounded-full` · 底 `neutral-200/80`；滑块 **纯几何定位**：`left: calc(2px + i*(100% - 4px)/n)`、`width: calc((100% - 4px)/n)`，`transition 150ms cubic-bezier(.32,.72,0,1)`；滑块自带 `inset 0 0 0 1px rgba(0,0,0,.06), 0 1px 2px rgba(0,0,0,.08)`；非激活标签字重写作 `font-[470]`；容器 < **254px** 时非激活项只留图标 |
| **Attachment Tiles** | 卡片 `w-32 h-24 rounded-xl`，横向排列 `overflow-x-auto`；入场 `file-thumbnail-grow-in` 0.25s；角标 CODE + 移除 `×` |
| **Code Block** | 容器 `bg-neutral-900` 深色，header `bg-neutral-950/70` + `fileCode` + 语言名；复制成功后对勾跑 `saved-check-decay 0.3s ease-out` 且 `delay 2.5s forwards`（即 2.5s 后淡出） |

## Motion Lab（三个交互实验室，非组件，是"可调参数演示"）

**Grapheme Reveal**：逐 **grapheme**（`Array.from(text)`，不是 code unit）套 `<span class="inline-block whitespace-pre">`，`animation: 90ms cubic-bezier(.215,.61,.355,1) both`，级联延迟默认 **30ms/字符**，滑杆范围 10~80ms。

**Quiet Dots**：放大版复用同一关键帧，`size-2`(8px) 圆点、`gap-[6px]`、延迟 `0/.15s/.3s`。

**Spring Physics Presets**（标 `AST-Extracted`，站点称"从桌面端逆向提取"）

| 预设 | stiffness | damping | mass | 声称超调 | 声称收敛 | `rawConfig` |
|---|---|---|---|---|---|---|
| Button & Small Controls | 600 | 30 | 1 | +4.2% | 0.22s | `{type:spring, stiffness:600, damping:30}` |
| Card / Popover Reveal | 320 | 28 | 1 | +1.8% | 0.35s | `{type:spring, stiffness:320, damping:28}` |
| Bouncy Light Feedback | 380 | 18 | 1 | +19.4% | 0.58s | `{type:spring, duration:0.32, bounce:0.15}` ⚠️ 与前四列不是同一套参数 |
| High Damping Panel Drag | 1200 | 80 | **0.1** | 0% | 0.14s | `{type:spring, stiffness:1200, damping:80, mass:0.1}` |

示波器用二阶阻尼振子解析解（欠/临界/过阻尼三分支）画位移曲线；自定义调参范围 `stiffness 50~1500 step10` · `damping 5~100 step1` · `mass 0.1~3.0 step0.05`。

---

## 取值注意（这份文件里的坑）

| 项 | 事实 | 怎么用 |
|---|---|---|
| `--cds-rem-scale` / `--cds-dur-*` / `--cds-ease-*` / `--cds-border` 等 `--cds-*` 命名 | 页面里**没有任何 `:root` 定义**（全文仅 10 处提及，全是文案 / 代码片段） | 作者自拟的叙述性命名，**不是真实变量名**，不要当 token 契约引用 |
| 字体栈 | 站点用 `Inter` / `Newsreader` / `JetBrains Mono`；官方是 `Anthropic Sans Text` / `JetBrains Mono` | 字号 / 行高阶梯可参考，字体名以官方 UI kit 为准 |
| `Bouncy` 预设参数 | 表里写 `380/18/1`，`rawConfig` 却写 `duration:0.32, bounce:0.15` | 两套参数体系不自洽，取用前先跑一遍看运动是否合理 |
| `533` 图标字重 / `+4.2%` 超调 / `254px` 折叠阈值 | 站点没给证据链（无文件哈希、无原始 CSS 摘录、无版本号） | **孤立数值**，别直接抄进页面；要用就标"自定" |
| 用户气泡底色 `#f0f4f8` | 偏蓝的中性色，与该站自己主张的暖灰 / 陶土色系不一致 | 疑似未校准，配色以官方 Colors 表为准 |

站点在**布局 / 几何 / 动效实现层**（圆角阶梯、控件高度、双环阴影、Composer 尺寸、Segmented 像素几何、关键帧、弹簧参数）给了一套自洽且可运行的数值，当**重建参考**可用；官方 UI kit 缺的正是这一层。

## 方法论教训

1. **单文件 SPA 站点，源码 > DOM > 截图**。数值以字面量写在 JSX 里时，直接下载 HTML + grep，比开浏览器逐页截图快一个数量级，而且没有 OCR/目测误差。
2. **按 UTF-8 落盘再读**。用 `Invoke-WebRequest` 默认编码写文件会把中文全变 mojibake（`ç¹å»`），要显式设 `Encoding = UTF8`。
3. **区分"页面自称"与"页面证明"**。`Verified from Claude.app` / `100% Fidelity` / `AST-Extracted` 都是作者写的徽标文案，不是证据；`--cds-*` 变量名看着像 token 契约，实际一条都没定义。
4. **去 `<style>` 里找动效全集**。token 展示页只会列出 3 条曲线，真正的曲线和 keyframes 散落在实现代码中（本页 11 个 keyframes、8 条曲线），只看 "Motion Tokens" 那张表会漏掉大部分。
5. **总数与清单必须点数核对**。本页旧稿的"9 个 keyframes"与清单里的 11 条对不上——凡是给了"共 N 个"的地方，都要逐条数一遍；对不上就说明整理过程有损耗，该处所有数值的可信度都要下调。
6. **两份蓝本落地前对照一次**。同一层数值在官方 UI kit 与社区逆向之间可能不一致（字重补偿、逐字动画），冲突处以官方为准，社区的只补官方空白的那几层。
