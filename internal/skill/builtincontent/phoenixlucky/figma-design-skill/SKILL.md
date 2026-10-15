---
name: figma-design-skill
version: 2.4.2
category: 前端与设计
source-url: https://github.com/phoenixlucky/figma-design-skill
runAs: inline
description: 给 AI 用的软件界面设计规范 skill，蓝本是 Claude 官方 UI kit——取出 Claude 的 UI 细节（变量 token、组件几何、动效数值、硬规则），并照着它直接改或做软件页面：产出可落地的代码（HTML/CSS/组件）或逐层改稿方案。当用户要求"按 Claude 的界面风格做/改页面""提取 Claude UI 的细节/token/组件数值"时使用。
---

# 照 Claude 的 UI 细节改 / 做软件页面

> 这个 skill 只做三件事：**呈现 Claude 的 UI 细节**（token / 组件几何 / 动效数值 / 硬规则）、**照着它改**已有软件页面、**照着它做**新页面。最后交出的是**页面**——代码或逐层改稿方案，不是设计赏析。

| 项 | 值 |
|---|---|
| 蓝本 | Claude 官方 UI kit：`references/anthropic-mcp-apps-design.md`（颜色 37 / 字体 29 / 几何 15 个变量 + 官方指南硬规则） |
| 次选蓝本 | `references/claude-design-system-storybook.md`——第三方逆向的 Claude 桌面端 Storybook，覆盖 7 个基础规范、7 个组件和 3 个动效实验；圆角、高度、阴影、排版、图标及动效实现可补官方空白，**不是官方来源**，用了要标明 |
| 目标 | 你要改、或要做的那个软件页面 |
| 交付 | 页面代码（HTML+CSS / 组件）或逐层改稿 diff；用户只要细节时，给细节表 |
| 底线 | 页面里每个数值，要么来自蓝本实读，要么显式标"蓝本没有，自定" |

## 何时使用

- 要按 Claude 的界面风格**做新页面**，或**改造现有页面**（配色、字号、圆角、层级、动效对齐）
- 用户贴 Figma 链接（`figma.com/design/...` 或 `figma.com/community/file/...`），要求"提取设计细节""看它藏了什么规范""导出 token"
- 用户贴的是**网页**版设计系统（Storybook 站点、把某产品的 UI 逆向成文档的 SPA）——取法与 Figma 相反：**源码 > DOM > 截图**，先读 `references/claude-design-system-storybook.md` 的"提取方法"节
- 要校某个具体数值：圆角阶梯、控件高度、双环阴影、行高节奏、字重、弹簧参数

## 蓝本里有什么（先看这张，别另起炉灶）

| 层 | 蓝本里的东西 | 落到页面上是什么 |
|---|---|---|
| 颜色 | 37 个变量，**Light / Dark 双 mode**：Background / Text / Border / Ring | CSS 变量两套 + 主题切换 |
| 字体 | 29 个变量：`Anthropic Sans Text` / `JetBrains Mono`；正文四档 12 / 14 / 16 / 18，行高统一 1.4；标题七档，行高逐档收紧 1.4 → 1.25 → 1.1 → 1.0 | `font-size` / `line-height` 阶梯 |
| 几何 | 15 个变量：桌面圆角 4 / 6 / 8 / 10 / 12 / 999；移动独立梯度 0 / 2 / 4 / 8 / 10 / 14 / 24 / 9999；描边 `0.5` 发丝线 | `border-radius` / `border-width` |
| 间距 | **没有 token**，内嵌在组件 Auto layout 里（如 Inline card：gap 128 / padding 128） | 组件级 gap / padding，别硬造一套 spacing 体系 |
| 动效 | 官方 UI kit **没有**任何 prototype 交互 | 要曲线 / 关键帧就取次选蓝本，或自定并标明 |
| 硬规则 | 官方指南的九条（内联卡片、移动端、视觉、动效、字体） | 直接当约束写进页面 |

网页蓝本取值按源码优先：读取 Storybook 的内联 CSS / JSX，再用页面 DOM 核对条目与展示值；不要把站点的 `Verified from Claude.app`、`100% Fidelity` 或 `AST-Extracted` 徽标当作官方证据。具体 token、组件与动效值见次选蓝本参考文件。

### 目标项目字体默认值

- 英文：`Inter, sans-serif`
- 中文：`Inter, "Source Han Sans SC", "Noto Sans SC", sans-serif`（思源黑体 / Noto Sans SC）
- 这是目标项目的字体偏好，不是 Claude 官方 UI kit 的字体 token；若目标项目已有字体规范，沿用项目规范。

## 核心认知：Figma 内容分三层，取法完全不同

| 层 | 内容 | 能否读 DOM | 取法 |
|---|---|---|---|
| 变量 / 样式 | Variables 面板里的 Border / Colors / Typography 集合 | **能**，是普通 DOM 表格 | CDP 读 `input.value` |
| UI 面板 | 图层面板、右侧 Design / Prototype 面板 | **能**，是普通 DOM | 选中节点后读 `innerText` |
| 画布 | 画布上的图形与文字 | **不能**，画布是 `<canvas>` | 只能截图，靠视觉读 |

这条决定全部策略：**能读 DOM 的绝不要靠截图猜；画布内容只能截图看。**

## 第 0 步：先判来源类型，再选取法

取法由**来源类型**定，不由 URL 定。归类错了，后面全白做：

| 来源 | 识别特征 | 取法（优先级从高到低） |
|---|---|---|
| Figma 设计文件 | `figma.com/design/…`、`/community/file/…`、`/proto/…` | 变量面板 / 右侧面板 **DOM** > 画布**截图** |
| 单文件网页（Storybook、逆向文档 SPA） | 一个 HTML 装下全部内容，数值以字面量写在 JSX/JS 里 | **源码**（下载 HTML + 检索）> DOM `innerText` > 截图 |
| 常规文档站 / 规范页 | 多页静态或 SSR 站点，正文本身是文本 | **页面正文**（`chrome/chrome_page_load_read` 或 `web_fetch`）> 截图 |
| 组件库源码仓库 | GitHub / npm 包 | **源码**（token 文件、CSS 变量、`@theme` 定义）> 文档 |

代价对比：对单文件 SPA 逐页截图，比读源码慢一个数量级且有目测误差；对 Figma 只读源码，则拿不到面板与画布上的真实值。

## 前置条件

不需要 Figma 官方 MCP、不需要 access token。用本地 Chrome MCP 就够：

- `chrome/chrome_page_navigate` 打开链接（Community 文件免登录可读）
- `chrome/chrome_script_execute` 读 DOM（走 CDP `Runtime.evaluate`）
- `chrome/chrome_page_screenshot` 截画布
- `chrome/chrome_page_read_accessibility` 拿元素 ref（需要真实点击时用）
- `chrome/chrome_computer_act` 真实鼠标键盘（`action=left_click` / `key` / `scroll`）

工具名与参数**以实时 `tools/list` 为准**，不要凭记忆写。上面五个是本流程会用到的，够用就好。

## 标准流程（把蓝本取到手）

### 1. 打开并读变量面板（信息密度最高）

带 `view=variables` 的链接会直接进变量面板。先枚举左侧 Collections 列表：

```js
[...document.querySelectorAll('button[data-testid="localVariableSetRowButton"]')]
  .map(b => b.textContent.replace(/\s+/g,' ').trim())   // 例：["Border15 variables15", ...]
```

再逐个点开读值。**关键：用同一行里 `input` 的数量判断它有几种 mode。**

```js
(async () => {
  const btns = [...document.querySelectorAll('button[data-testid="localVariableSetRowButton"]')];
  btns.find(b => b.textContent.includes('Colors')).click();
  await new Promise(r => setTimeout(r, 1600));
  const t = document.querySelector('[data-testid="variables-modal-table"]');
  return JSON.stringify([...t.querySelectorAll('tbody tr')].map(r => {
    const name = (r.querySelector('th')?.innerText || '').replace(/\s+/g,' ').trim();
    const vals = [...r.querySelectorAll('input')].map(i => i.value);
    return `${name || '<group>'} | ${vals.length} | ${JSON.stringify(vals)}`;
  }));
})()
```

一行 1 个 input = 单一 mode；一行 2 个 = Light/Dark 两列；颜色带透明度时会是 4 个（hex、alpha、hex、alpha）。**这是判断"某 token 有没有暗色分支"的硬依据**，不要凭截图猜。

### 2. 枚举全部页面

左侧 Pages 面板里的行按钮，class 含 `pageRowButton`：

```js
[...document.querySelectorAll('button')]
  .filter(x => (x.className||'').toString().includes('pageRowButton'))
  .map(p => (p.innerText||'').replace(/\s+/g,' ').trim())
```

注意：名字为空的行是分隔项；空页面（如 `Foundations`、`Displays`）常被作者用作分组标题，进去是空的，别当成内容页。

### 3. 逐页截图看画布

画布文字读不到，只能截图。切页 + 适配 + 截图：

```js
(async () => {
  const ps = [...document.querySelectorAll('button')]
    .filter(x => (x.className||'').toString().includes('pageRowButton'));
  ps.find(p => /Inline card/.test(p.innerText)).click();
  await new Promise(r => setTimeout(r, 2200));
  // 用 zoom 菜单适配，不要指望快捷键
  const zb = [...document.querySelectorAll('button')]
    .find(b => /^\s*\d{1,3}%\s*$/.test((b.innerText||'').trim()));
  zb.click(); await new Promise(r => setTimeout(r, 700));
  [...document.querySelectorAll('div,button,li,span')]
    .find(e => /^Zoom to fit/.test((e.innerText||'').trim())).click();
  await new Promise(r => setTimeout(r, 1000));
})()
```

### 4. 要精确数值时选中节点，读右侧面板

**点图层面板行选中节点**，然后读右侧 Design 面板（`[class*=properties_panel]` 的 innerText），能拿到 X/Y、W/H、Auto layout 的 flow / Hug / Gap / Padding / Clip content、Fill、Stroke、Effects 等精确值。

图层行是 `[role="gridcell"]`。⚠️ 合成事件可能被 Figma 拒绝（见陷阱 2），优先用 CDP 真实点击。

### 5. 判断有没有原型 / 动效

切 `Prototype` 标签（右上，是 `SPAN`，点击会冒泡到父级）：

- 右侧显示 `Prototype settings` + `Creating a connection` / `Running your prototype` 帮助文案 = **当前对象没有交互**
- 画布上没有任何蓝色连线 = 该页没有交互
- 文件里没有 prototype，就**不存在 easing / 贝塞尔曲线**可提取，如实说；要动效就取次选蓝本，或自定并标明

## 照 Claude 的细节改 / 做页面

### 1. 先建映射表：目标页面的每个维度 ↔ 蓝本的一行

| 目标页面维度 | 蓝本取值 | 落到页面的值 |
|---|---|---|
| 页面 / 卡片底色 | Colors · Background Primary / Secondary | `--bg: #FFFFFF` / `#F5F4ED` |
| 正文与标题字号 | Typography · Size Text / Size Heading | 14 / 16 / 18 / 20 / 24 … |
| 行高 | 正文四档统一 1.4；标题逐档 1.4 → 1.25 → 1.1 → 1.0 | `line-height` |
| 描边与分层 | Border 全系列靠透明度 15% / 30% / 40%，Width 只有 0.5 | `border: 0.5px` + 半透明色 |
| 圆角 | Border · Radius（桌面档与移动档是两条独立梯度） | 4 / 6 / 8 / 10 / 12 |
| 焦点环 | Ring 70% / 50% | `outline` / `box-shadow` |
| 控件高度、间距 | 官方 UI kit **没有间距 token** | 取次选蓝本的 Heights，或自定 |
| 动效 | 官方 UI kit **没有原型** | 取次选蓝本的 keyframes / 曲线 / 弹簧参数 |

### 2. 两种交付形态

- **出代码**：把蓝本落成 CSS 变量 + 字面量。颜色走变量（`--bg` / `--text-primary` / `--border-secondary`…），几何值写字面量并**在注释里标出处**：

  ```css
  border-radius: 6px;   /* Border/Radius SM（桌面档） */
  border: 0.5px solid color-mix(in srgb, #1F1E1D 30%, transparent);  /* Border/Secondary */
  ```

- **出改稿**：给 diff 表，一行一处改动——`元素 | 现值 | 改成 | 蓝本依据`；改动密集处附最小代码片段。

### 3. 落值口径（三条）

1. **结构照搬，品牌可换**：几何、节奏、层级、字重阶梯、描边分层照蓝本的数；品牌色 / 文案 / logo 按目标改——官方指南也只允许在 accent / 品牌色上表达自己。
2. **蓝本没有的必须标**：间距体系、动效曲线在官方 UI kit 里**不存在**。要用就写明取自次选蓝本，或标"自定"。不要借蓝本的名义写自己的数。
3. **两蓝本冲突时以官方为准**：官方 UI kit 与社区逆向冲突（字重补偿、逐字动画），采用官方；社区的只用于官方没覆盖的层（像素几何、关键帧、弹簧参数），并注明是社区来源。

### 4. 交付前自检

- 页面里每个数值都能对上蓝本某一行，或已标"自定"
- Light / Dark 两套都出（蓝本 Colors 是双 mode）
- 移动档用移动梯度（圆角梯度、`Weight Mobile 550`），不是桌面档
- 触控目标 ≥ 44pt；加载态用贴合内容的 **skeleton** 而非 spinner；内联场景不抢滚动手势

### 5. 图标生成规范

- **生成方式**：新图标默认使用生图工具绘制，不用 SVG 路径或外部图标包替代。需要一组图标时，按统一风格生成，再裁切成单个图标资源；只有生图工具不可用，或无法满足图标所需的清晰度与精度时，才考虑手绘 SVG。
- **统一存放与引入**：图标资源放在 `public/assets/icons/`，按目标项目的组件方式组成并引入项目图标库；页面从库中引用，不散落重复资源。文件名对应清晰语义。
- **保持一致**：同一图标库使用统一的画布尺寸、轮廓、端点、填充方式和视觉重量。蓝本无法决定目标项目的图标风格，不要为了 Claude 风格混用另一套图标。
- **再定语义**：每个图标只表达一个对象或动作；优先选熟悉、可辨认的符号，不用 emoji、文字字符或装饰性细节代替功能图标。含义不明确时给控件加可见文字标签。
- **生成与裁切**：尽可能生成透明背景的图标图像，裁切为单个、边界整齐的资源后放入图标目录；按使用尺寸检查清晰度，避免放大低分辨率图。若触发 SVG 兜底，保持统一 `viewBox`、轮廓、端点和填充方式，并说明兜底原因。
- **尺寸**：先沿用目标项目的尺寸档；没有既有规范时，按目标控件定尺寸并标“自定”。仅在明确采用社区逆向蓝本时，才可参考其 Phosphor 图标的 12 / 14 / 16 / 20 / 24px 阶梯，并标注“社区逆向，非官方”。这些不是 Claude 官方 token。
- **对齐与无障碍**：与文字并排时按实际字形做光学居中，不机械套用社区逆向的基线偏移；可点击图标需有可访问名称，纯装饰图标标记为隐藏；图标按钮的触控目标仍须 ≥ 44pt，不能只按图形本身计算。
- **交付**：说明图标语义、来源、尺寸档与交付格式；新增图标附裁切后的图片资源和库内引用。检查实际尺寸下清晰、浅色 / 深色可见、状态不只靠颜色区分。

### 6. 背景图与插画生成规范

- **统一存放**：配图放在 `public/assets/` 下用途对应的目录，如 `backgrounds/`、`illustrations/`；文件名描述用途或页面区块。
- **生成与裁切**：需要新配图时使用生图工具按目标页面的主题、风格和构图生成；再根据页面容器和断点实际裁切，输出可直接引用的成品到对应资产目录。不要只靠 CSS `cover` 把未检查的大图当作最终交付。
- **先判断是否需要图片**：背景图只在能帮助表达品牌、场景或内容层级时使用；纯装饰不应降低文字和控件的辨识度。能用现有 CSS、渐变或项目资产清楚表达时，优先复用它们。
- **先定画面用途与构图**：生成前写清使用位置、主题、风格、目标比例、主体位置和文字留白区。首屏标题所在区域保持低细节、低对比；避免把关键主体放在可能被裁掉的边缘，也不要让生成图包含文字、按钮、伪 logo 或 UI 截图。
- **适配布局**：按实际容器确定比例和裁切方式；桌面与窄屏分别检查主体、留白和焦点位置，必要时使用不同裁切或单独资产。背景用 `cover` 时须检查裁切结果，别假设中心裁切总是合适。
- **确保前景可读**：在背景上叠放文字前检查最亮、最暗和主体细节区域的对比；按需加宿主 token 颜色的遮罩或渐变，并同时检查浅色 / 深色主题。图片不能承担文字信息或唯一状态提示。
- **格式与交付**：默认使用生图工具输出的栅格图像；按用途交付合理尺寸与格式，需要透明背景的主体资产使用透明图。只有生图工具不可用或无法满足精度要求时，才考虑 SVG。注明生成工具 / 来源、提示词要点、比例、裁切锚点及任何自定颜色或视觉参数，并在页面代码中从 `public/assets/` 引用成品。
- **检查与无障碍**：预览实际页面中的桌面和窄屏效果；确认压缩后清晰、焦点未被裁切、文字可读。纯装饰背景对辅助技术隐藏；承载信息的图片提供简短替代文本。

## 产出模板

### 段 1 · 蓝本与目标

| 项 | 值 |
|---|---|
| 蓝本 | 官方 Claude UI kit / 次选社区逆向（标明） |
| 目标 | 要改或要做的页面（文件路径 / 链接） |
| 交付形态 | 代码 / 改稿 diff / 两者 |
| 覆盖范围 | 哪几层对齐（颜色 / 字体 / 几何 / 动效） |

### 段 2 · 映射表

照「照 Claude 的细节改 / 做页面」第 1 节的三列表格填：**目标维度 | 蓝本取值 | 落到页面的值**。一个维度一行，数值带单位。

### 段 3 · 产出

代码给可运行代码块；改稿给 diff 表。**代码里的关键数值带注释标出处**，例：`/* Border/Radius MD */`。

### 段 4 · 蓝本里没有的（自定项）

逐条列出：蓝本没有的层（间距、动效曲线）、自定的值、取自次选蓝本的项及其来源属性（社区逆向，非官方）。

### 用户只要细节表时

不出页面，给三样：**token 表**（面板实读的精确值，一行一个变量并标明 mode 数）/ **硬规则**（官方指南或文件里明写的约束，定性与定量分列）/ **蓝本里没有的**（缺哪些 token 集、有没有暗色分支、有没有原型动效）。

格式口径：

- 颜色统一写 `#RRGGBB`；面板读出的是裸 hex（`FFFFFF`）时补上 `#`，并说明"面板原样为裸 hex"。
- 带透明度的值单独注 alpha（如 `#1F1E1D` 40%）。
- 数值写单位（px / pt / rem）；换算值（如"行高 16.8 ÷ 12 = 1.4"）与实读值分列，别混进同一列。
- 表达口径：中文、表格优先、先结论后依据；数值表可以长，推论必须短。
- 每份产出末尾留「方法论教训」节。

## 失败与降级

| 情形 | 处理 |
|---|---|
| chrome MCP 不可用 | 网页源优先 `web_fetch` / `chrome/chrome_page_load_read` 直取源码或正文；只差 token 且用户有 access token 时才是 Figma REST（`/v1/files/:key/variables/local`）；都没有就如实说明缺什么，**不要凭印象编数值** |
| 选择器失效 | Figma 内部混淆名会随版本变。按 `data-testid` → `role` / `aria-*` → 可见文案 → 坐标点击逐级降级，见 `references/browser-recipes.md` 的"选择器失效时的降级顺序" |
| 合成事件不生效 | 改用 `chrome/chrome_computer_act` 真实点击（陷阱 2） |
| 窗口最小化 / `FOREGROUND_REQUIRED` | 先走 `background=true` 截图与非交互取数；只有必须真实点击时才恢复前台（陷阱 3），并在回答里说明打扰 |
| 怀疑页面没加载出来 | 先确认是不是"空分隔页"（陷阱 9），再重试一次；连续两次同样结果就换一层取数，别反复重试 |
| 蓝本没有要用的层 | 官方 UI kit 没有间距 / 动效 → 取次选蓝本并注明社区来源，或自定并标注；**不要静默编一个值** |
| 工具名 / 参数不确定 | 以实时 `tools/list` 为准 |

## 陷阱清单（都是踩过的）

1. **Community 文件免登录可读**。不要因为"需要 Figma 登录"就放弃；先用浏览器打开确认。
2. **合成事件对部分 Figma 组件无效**。图层行等用 `pointerdown` 且校验 `isTrusted`，`el.click()` 或手动 `dispatchEvent` 派发的 `PointerEvent` 很可能被忽略，表现为"点了没反应、右侧面板不变"。→ 用 `chrome_computer_act action=left_click` 真实点击。
3. **真实坐标点击要求窗口不是最小化**，否则报 `FOREGROUND_REQUIRED`。最小化时先用 Win32 恢复：

   ```powershell
   Add-Type -Namespace Win -Name U -MemberDefinition '[DllImport("user32.dll")] public static extern bool ShowWindow(System.IntPtr hWnd, int nCmdShow); [DllImport("user32.dll")] public static extern bool SetForegroundWindow(System.IntPtr hWnd);'
   Get-Process chrome | Where-Object { $_.MainWindowHandle -ne 0 } | ForEach-Object { [void][Win.U]::ShowWindow($_.MainWindowHandle, 9); [void][Win.U]::SetForegroundWindow($_.MainWindowHandle) }
   ```

   这会抢前台，属于对用户桌面的打扰——能不用就不用，用了要在回答里说明。
4. **键盘快捷键不可靠**。`Shift+1`（zoom to fit）、`Shift+2`（zoom to selection）经常不生效。→ 用 zoom 菜单：点右上角百分比 → `Zoom in` / `Zoom to fit`。想放大就循环点 `Zoom in`（8% → 13% → 25% → 50% → 100% → 200%）。
5. **坐标空间会缩放**。截图 PNG 尺寸 = viewport × dpr（常见 ×1.25）。传坐标给 `chrome_computer_act` 前先确认用的是哪个空间，否则点偏。
6. **切标签后截图可能截到别的标签页**。多标签操作时先 `chrome/chrome_tab_activate`，再截图。
7. **窗口反复被最小化**时不要反复抢前台，改用 DOM 能读到的层（变量面板、右侧面板）+ `background=true` 截图，收敛结论。
8. **变量面板里看不到间距体系**。很多 kit 根本没有 spacing token（间距内嵌在组件的 Auto layout 里），这点要明说，别把 radius 表当成完整 token 集。
9. **页面空 ≠ 加载失败**。作者会把空页面当分组标题用，先看 Layers 面板是否为空再判断。
10. **选择器是 Figma 内部混淆名，会随版本变**。`localVariableSetRowButton`、`pageRowButton`、`.ublptto`、`[class*=properties_panel]` 都属于实现细节，升级后可能失效。写脚本前先在 Console 手验一次；失效就按降级顺序退，**不要把选择器当成 API 契约**写进结论。
11. **交付前逐项对账**。页面或表里的每个数值，都要能指到蓝本哪一行；指不出来就标"自定"或删掉。最容易混进来的是"看着合理"的精确值（`460/560`、`90ms` 这类）。
12. **社区逆向蓝本只补官方空白，且必须标明来源**。它和官方 UI kit 冲突时（字重补偿、逐字动画），一律采用官方。

## 参考文件

| 文件 | 定位 |
|---|---|
| `references/anthropic-mcp-apps-design.md` | **主蓝本**：Claude 官方 UI kit 的全套变量 token、组件 Auto layout 实测、官方指南硬规则、动效缺失情况 |
| `references/claude-design-system-storybook.md` | **次选蓝本**：第三方逆向的「CDS Storybook」站点的 token / 组件规格 / 关键帧 / 弹簧参数。**社区来源，非官方**，只补官方空白处。**来源是网页而非 Figma 时，先读这份的"提取方法"节** |
| `references/browser-recipes.md` | 可复制的 DOM 选择器与操作序列。**代码以这份为准**，SKILL.md 里的片段是精简版 |

## 维护约定

- 本目录是**源目录**，已安装的 skill 是副本。改完源文件要同步安装，否则跑的还是旧版；步骤见 `README.md`。
- 新产出**按新文件追加**到 `references/`，不要改写既有产出——既有的那份是页面落值时的对照物。
- 每份产出末尾都要留「方法论教训」节：把这次踩到的坑沉淀成下一条陷阱或降级路径，而不是只留结论。
- 产出是大量表格，**单元格里不要出现连续的 `||`**（JS 里的 `a||b`）——GFM 会把它当列分隔符，把表格撑裂；改用 `String(a).trim()` 之类写法。改完用脚本逐块核对表头与分隔行的列数一致。
