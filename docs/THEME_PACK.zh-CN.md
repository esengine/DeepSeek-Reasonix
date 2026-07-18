# Reasonix 主题包 V2

Reasonix 桌面端原生主题包。主题是**受控皮肤**：语义颜色令牌、密度/圆角配方，以及首页与任务/工作区可独立配置的本地背景图。主题**不能**执行 CSS、JavaScript、加载字体、远程 URL 或 SVG 脚本。V1 主题继续兼容，并在两个场景共用首页图片。

> English: [THEME_PACK.md](./THEME_PACK.md)

## 首版目标

- 内置风格、自建主题、背景图、实时预览、导入/导出、本地主题库
- 首页完整展示背景；进入任务后自动降低透明度并增加方向性遮罩
- 同时支持 Classic / Workbench / Creation，以及 `auto` / `light` / `dark`
- **不包含**在线主题市场、云同步或脚本插件

## 主题体验（设置信息架构）

外观拆成两个界面（不增加第三套入口）：

1. **外观概览页** — 当前主题摘要、明暗模式、**唯一**基础配色控件、字体与缩放。主操作：**浏览主题**。
2. **主题画廊** — 官方 / 我的主题 / 基础配色三个分类；点击卡片仅选中；详情区隔离预览；
   「临时预览」才覆盖全应用；只有「应用主题」会持久化。沉浸式预览是画廊详情的一部分。

状态模型（`desktop-theme-state.json` schema v2）：

| 状态 | 含义 | 持久化 |
| --- | --- | --- |
| `themeMode` | 自动 / 浅色 / 深色 | 桌面配置 |
| `baseStyle` | Graphite…Amber | 桌面配置（`theme_style`） |
| `activeThemeId` | 仅官方或用户主题 | `desktop-theme-state.json` |
| `selectedThemeId` / `previewThemeId` | 画廊选中 / 临时预览 | 仅前端内存 |

- `activeThemeId` **禁止**保存基础配色 id。选择基础配色会清空主题包。
- 应用主题包时保留 `baseStyle` 作为停用后的回退值。
- 明暗模式与主题包独立。

## 主题类型

画廊分三组：

| 类型 | 来源 | 可编辑 | 可删除 | 可导出 |
| --- | --- | --- | --- | --- |
| **基础风格** | 六种视觉方向（Graphite、Aurora、Slate、Carbon、Nocturne、Amber），无令牌覆盖 | 否（需先复制） | 否 | 否 |
| **官方主题** | 安装包内嵌的八款只读主题（清单 + 原创背景 + 缩略图，MIT） | 否（需先复制） | 否 | 否 |
| **我的主题** | 编辑器新建、复制，或导入 `.reasonix-theme` | 是 | 是 | 是 |

- 14 个内置 id（6 基础 + 8 官方）均为**保留名**：保存、导入、覆盖复制与删除都会拒绝冲突。
- 激活官方主题时，仅把其 id 写入 `desktop-theme-state.json`；资源在运行时从嵌入副本读取。
- 对基础/官方主题执行「复制」会生成可编辑的用户主题（官方背景会拷入用户库），之后可编辑或导出。
- v1 若把基础配色 id 存为 `activeThemeId`，加载时会迁移到 `desktop.theme_style` 并清空活动主题。

### 八款官方主题

| ID | 名称 | 基础风格 | 画面 |
| --- | --- | --- | --- |
| `official-rose-dawn` | Rose Dawn / 玫瑰晨光 | graphite | 象牙白晨光、柔粉玫瑰、原创插画女性 |
| `official-fortune-forge` | Fortune Forge / 鸿运工坊 | amber | 朱红/金/玉绿工坊、原创吉祥程序员 |
| `official-crimson-horizon` | Crimson Horizon / 赤曜新城 | graphite | 珊瑚红未来城市天际线，无人物 |
| `official-sage-breeze` | Sage Breeze / 鼠尾草清风 | slate | 奶油纸张、鼠尾草、原创读者 |
| `official-spark-notebook` | Spark Notebook / 灵感手账 | aurora | 手账网格与文具、原创动漫成年人 |
| `official-violet-starlight` | Violet Starlight / 紫曜星夜 | nocturne | 蓝紫星空、蝴蝶、剪影女性 |
| `official-cyan-stage` | Cyan Stage / 青岚舞台 | carbon | 青蓝舞台与光环、原创数字表演者 |
| `official-noir-gold` | Noir Gold / 黑金序曲 | carbon | 黑丝绒、金色聚光灯、原创绅士 |

预览在应用内主题库（设置 → 外观）中展示，来自真实 Reasonix 构建。**请勿把应用截图当作主题背景导入。** 素材来源、哈希与许可记录见 [THEME_ASSETS.zh-CN.md](./THEME_ASSETS.zh-CN.md)；生成脚本在 `scripts/official-theme-art/`（程序化、固定种子、可复现）。

## 包格式

以 `.reasonix-theme` ZIP 分发。根目录**只能**包含：

| 文件 | 必需 | 说明 |
| --- | --- | --- |
| `theme.json` | 是 | 清单（≤ 1 MiB） |
| `background.png` / `.jpg` / `.jpeg` / `.webp` | 否 | 首页图片 ≤ 16 MiB，边长 ≤ 8192 |
| `background-task.png` / `.jpg` / `.jpeg` / `.webp` | 否 | 独立任务/工作区图片 ≤ 16 MiB，边长 ≤ 8192（V2） |

ZIP 限制：包体 ≤ 36 MiB；禁止子目录、符号链接、重复条目与路径穿越。

### `theme.json` 示例

```json
{
  "schemaVersion": 2,
  "id": "my-theme",
  "name": "My Theme",
  "author": "",
  "description": "",
  "license": "",
  "baseStyle": "graphite",
  "tokens": {
    "light": {
      "bg": "#f4f3ef",
      "fg": "#111827",
      "accent": "#2f5fa8"
    },
    "dark": {
      "bg": "#0c0d10",
      "fg": "#f1f1ef",
      "accent": "#ff6a3d"
    }
  },
  "recipes": {
    "density": "comfortable",
    "corners": "soft"
  },
  "background": {
    "image": "background.webp",
    "focusX": 0.72,
    "focusY": 0.45,
    "safeArea": "left",
    "homeOpacity": 1,
    "taskOpacity": 0.28,
    "overlayStrength": 0.62
  },
  "taskBackground": {
    "image": "background-task.webp",
    "focusX": 0.5,
    "focusY": 0.5,
    "safeArea": "right",
    "opacity": 0.28,
    "overlayStrength": 0.62
  }
}
```

JSON Schema： [theme-pack.schema.json](./theme-pack.schema.json)

### 字段规则

| 字段 | 规则 |
| --- | --- |
| `schemaVersion` | `1` 或 `2`；使用 `taskBackground` 时必须为 `2` |
| `id` | 小写 `[a-z][a-z0-9-]*`；保留：`graphite`/`aurora`/`slate`/`carbon`/`nocturne`/`amber` |
| `baseStyle` | 六套内置方向之一；未覆盖令牌继承该方向 |
| `tokens.light` / `tokens.dark` | 可选；键 → `#RRGGBB` 或 `#RRGGBBAA` |
| `recipes.density` | `compact` \| `comfortable` |
| `recipes.corners` | `square` \| `soft` \| `round` |
| `background.image` | 仅允许裸文件名（png/jpeg/webp） |
| `background.focusX/Y` | 0–1 焦点 |
| `background.safeArea` | `left` \| `right` \| `center`（任务页遮罩方向） |
| `background.homeOpacity` | 0–1 |
| `background.taskOpacity` | 0–1 |
| `background.overlayStrength` | 0–1 |
| `background.paneOpacity` | 0–1（首页场景面板不透明度） |
| `taskBackground.image` | 可选的独立任务/工作区图片，仅允许本地裸文件名 |
| `taskBackground.focusX/Y` | 0–1 焦点 |
| `taskBackground.safeArea` | `left` \| `right` \| `center` |
| `taskBackground.opacity` | 0–1 |
| `taskBackground.overlayStrength` | 0–1 |
| `taskBackground.paneOpacity` | 0–1（任务场景面板不透明度） |

### 允许的令牌键

`bg`, `bgSoft`, `bgElev`, `panel`, `sidebar`, `chat`, `workspace`, `workspaceFiles`,
`border`, `borderSoft`, `fg`, `fgDim`, `fgFaint`, `accent`, `accentFg`, `ok`, `warn`, `err`

颜色**不得**包含 `url()`、渐变或任意 CSS。

## 引擎行为

1. 先应用全局 `auto`/`light`/`dark` 与基础视觉风格。
2. 再应用主题包覆盖层（CSS 变量），挂在样式表之后，避免被后置 `:root` 与 Creation 局部变量压掉。
3. 根节点 `data-theme-pack="<id>"`；应用容器 `data-theme-scene="home|task"`。
4. 场景仅由当前会话是否有内容决定，不改变聊天状态或布局生命周期。
5. 背景为独立、不可交互层。任务页限制最高透明度并叠加方向性遮罩（**不**使用 `backdrop-filter`）。

## 存储

| Reasonix 主目录路径 | 用途 |
| --- | --- |
| `desktop-theme-state.json` | 版本化的当前主题指针（**不**改 `config.toml`） |
| `themes/<id>/` | 用户主题库（`theme.json` + 最多两张可选场景图片） |

旧配置缺少主题状态时保持原行为；旧版本忽略新目录。CLI 主题、提示词、Provider 请求与缓存键均不变。

## 桌面桥接

列出 / 启用 / 重置 / 保存 / 删除 / 复制 / 导入 / 导出 / 选择背景。
前端只接收临时资源 URL（`/__reasonix_theme_asset/...`）或 data URL，不暴露本机绝对路径。

同 ID 导入默认拒绝，确认后才允许原子替换。内置主题不可覆盖或删除。损坏/丢失回退 Graphite 路径。安全模式不加载外部主题。`/theme reset` 与命令面板可恢复默认。

## 创作建议

1. 从内置方向起步，只覆盖需要的令牌。
2. 尽量满足 WCAG AA（正文约 4.5:1）；编辑器会警告但允许继续保存。
3. 分享含背景图的主题前，确认照片/肖像/第三方素材的分发权利。
4. 首版不复制参考仓库的人物或第三方图片资产。

## 模板

无版权素材的纯色模板（不含背景图）见英文版 [THEME_PACK.md](./THEME_PACK.md) 中的 `paper-dawn` 示例；将仅含 `theme.json` 的根目录打成 `paper-dawn.reasonix-theme` 即可导入。
