# 右侧面板能力描述（zk-ge 能力问题清单）

> 依据 zk-ge-verification 方法：**业务层能力问题**（用户行为语言，零技术词，每个必须有产品入口）+ **技术层 claims**（可被推翻、可机器验证）。范围限定「右侧面板」（workbench dock：标签页 + 文件树 + 预览 + 搜索 + 面包屑），不含左侧栏、顶部 Tab 栏、聊天区。

## 一、能力范围（边界）

**包含**：右侧面板的标签页条（tab strip）、文件树、文件预览、路径面包屑、文件搜索、宽度拖拽、折叠/展开。
**不包含**：左侧会话栏、顶部 Tab 栏、聊天转录、设置面板、提交（changes）视图的业务逻辑（仅含其作为标签页的展示）。

**当前实现基线**：`feat/right-sidebar-redesign` 分支（rebase 至 upstream/main-v2 最新），含标签页拖拽排序、+ 按钮常驻、关闭按钮 hover 显隐、搜索目录树、全局树宽缓存等本地定制。

---

## 二、业务层能力问题（用户语言，零技术词）

> 每个问题回答「用户在哪个界面、做什么操作会触发」。8 维度扫描生成。

### 正常路径

| # | 能力问题 | 产品入口 |
|---|---|---|
| B01 | 用户能打开右侧面板，并看到面板里有哪些可用视图（文件/改动/概览） | 右侧面板收起态的工具条入口，点击展开 |
| B02 | 用户点开文件列表里的一个文件后，右侧能看到该文件的内容预览 | 文件树 → 点击文件名 |
| B03 | 用户能知道当前预览的是哪个文件、它位于哪个目录下 | 预览区顶部的路径面包屑 |
| B04 | 用户能在右侧面板里搜索一个名字，找到包含该名字的所有文件和文件夹 | 文件列表顶部的搜索框输入关键词 |
| B05 | 用户能新建、切换、关闭面板里的标签页 | 标签页条（+ 按钮 / 点击标签 / 关闭叉） |
| B06 | 用户能调整标签页的顺序 | 按住标签拖动 |
| B07 | 用户能调整「文件列表 / 预览」两侧的宽度比例 | 拖动中间的宽度分割线 |

### 边界输入

| # | 能力问题 | 产品入口 |
|---|---|---|
| B08 | 文件名很长时，标签和面包屑不能把界面撑破，长出的部分要能看清或省略 | 打开长文件名文件（如 `desktop_launch_config_test.go`） |
| B09 | 标签页很多、面板宽度不够时，「+」按钮不能消失，也不能盖住右上角折叠按钮 | 打开大量文件标签并收窄面板 |
| B10 | 搜索不存在的名字时，结果区域给出「无结果」而非报错或空白 | 搜索框输入乱码/不存在词 |

### 权限与越权

| # | 能力问题 | 产品入口 |
|---|---|---|
| B11 | 只读文件（或用户无写权限的文件）预览时不能被误改 | 预览只读文件 |
| B12 | 打开文件后，用户可以选择用外部程序打开（如默认应用 / 指定应用），由外部程序负责权限 | 标签右键菜单 → 打开方式 |

### 前置状态

| # | 能力问题 | 产品入口 |
|---|---|---|
| B13 | 面板收起时，用户仍能知道面板可以重新展开 | 收起态工具条 |
| B14 | 没有任何文件选中时，预览区给出「未选择文件」之类的提示，而不是空白报错 | 打开面板但不选文件 |
| B15 | 搜索状态下点击搜索结果，搜索列表保持（不自动退出搜索），用户能继续浏览其他结果 | 搜索 → 点击结果 |

### 并发与一致性

| # | 能力问题 | 产品入口 |
|---|---|---|
| B16 | 快速连续点击不同文件，预览稳定显示最后一次点击的文件，不出现错乱/闪跳 | 快速连续点击文件树多个文件 |
| B17 | 拖动标签换位过程中松开/中断，标签顺序不能错乱或残留异常位置 | 拖到一半松开、或拖动中按 Esc |

### 数据生命周期

| # | 能力问题 | 产品入口 |
|---|---|---|
| B18 | 关闭标签页后，该标签对应的预览随之关闭，其他标签不受影响 | 点击标签关闭叉 |
| B19 | 调整过的标签顺序、面板宽度，重启应用后保持 | 调整后重启应用 |
| B20 | 所有标签都关闭后，面板回到可用初始状态（不残留幽灵内容） | 逐个关闭所有标签 |

### 集成与扩展

| # | 能力问题 | 产品入口 |
|---|---|---|
| B21 | 面板里的「改动」视图作为标签页存在，其内容与主界面改动状态一致 | 点击「改动」标签 |
| B22 | 从文件树打开文件后，面包屑可复制该文件的路径（单段 / 完整路径） | 点击面包屑任意段 / 框选 |

### 安全与隐私

| # | 能力问题 | 产品入口 |
|---|---|---|
| B23 | 复制/打开的路径是用户当前工作区内的路径，不泄露工作区之外的信息 | 面包屑复制、右键复制路径 |
| B24 | 搜索结果不把 `node_modules`、`.git` 等生成目录的噪音带进结果 | 搜索常见词（如 `index`） |

---

## 三、技术层 claims（可验证声明）

> 每条 claim：oracle（机器可检查断言）+ counterexample（什么算错）+ risk_level。`scope` 限定右侧面板相关文件（`desktop/frontend/src/components/TabContainer/`、`WorkspacePanel.tsx`、`WorkspaceTreeRow.tsx`、`WorkspacePathBreadcrumbs.tsx`、`workspaceSplit.ts`、`workspaceTreeSearch.ts`、`store/layout.ts`、`internal/fileref/search.go` 等）。

| claim_id | statement | oracle | counterexample | risk |
|---|---|---|---|---|
| CLAIM.DOCK.001 | 标签拖拽换位后，新顺序写入持久化存储，重启后保持 | store 单测断言 moveTab 后状态被持久化（`activity-bar-store.test.ts` 31 项通过） | 重启后标签回到旧顺序 | R1 |
| CLAIM.DOCK.002 | 面板宽度不足以容纳全部标签时，「+」按钮保持可见且不与右上角折叠按钮重叠 | playwright 断言窄宽度下 add 按钮 `right <= tools.right` 且与 `.app__dock-toggle` 无相交；theme padding-right 44px / Windows 204px | 任何宽度下 add 按钮被挤出或与 toggle 重叠 | R1 |
| CLAIM.DOCK.003 | 标签关闭按钮默认隐藏，仅 hover / 激活标签时显示，且激活标签始终显示 | computed style 断言 resting `opacity: 0`、hover/active `opacity: 1` | 静止标签显示关闭叉，或激活标签不显示 | R0 |
| CLAIM.DOCK.004 | 标签标题超长时右侧渐变淡出而非省略号截断 | 断言 label `text-overflow: clip` 且 `::after` 渐变覆盖层存在；淡出终点色与 hover/active 背景一致（`--label-fade-end`） | 出现省略号，或淡出与背景色不一致 | R0 |
| CLAIM.WORK.001 | 文件树宽度为全局单值（跨标签共用），新建标签打开预览时读取该缓存宽度而非 50/50 | 断言 treeWidth/treeWidthMode 读写使用 `GLOBAL_TREE_WIDTH_KEY`；`workspace-split.test.ts` 通过 | 不同标签树宽不一致，或新建标签回退 50/50 | R1 |
| CLAIM.WORK.002 | 打开预览时树宽默认 200（无缓存时），预览区获得更多空间 | 断言 `WORKSPACE_TREE_DEFAULT_WIDTH = 200`、`initialWorkspaceSplitTreeWidth` 默认不再走 `panelWidth/2` | 首次打开预览树宽 300 或 50/50 | R1 |
| CLAIM.WORK.003 | 搜索结果按目录树展示：同名文件在多目录下各自出现，仅按名字命中的目录渲染为叶子（无展开箭头、不能展开成空） | 独立脚本复刻构建逻辑断言同名 2 份渲染、name-only 目录 `isOpen === undefined` | 同名文件丢失，或空目录可展开 | R2 |
| CLAIM.WORK.004 | 点击搜索结果打开预览时保持搜索状态（filter 不清空），清除搜索框才退出 | 断言 `selectFile` 不再调用 `setFilter("")`；filter 清空时 `collapsedSearchDirs` 重置 | 点击结果自动退出搜索，或下次搜索残留上次收起状态 | R1 |
| CLAIM.WORK.005 | 搜索结果包含 `.` 开头的隐藏文件/目录（除 `node_modules`/`.git`/`build` 等黑名单） | `go test ./internal/fileref/` 含 `TestSearchSurfacesHiddenEntries`；断言 `Search` 无 `showHidden` 门 | 搜索普通词看不到 `.cindy-worktrees` 等隐藏目录 | R1 |
| CLAIM.WORK.006 | 面包屑右对齐时文件名后缀完整可见（不依赖 scrollWidth，规避 WebKit flex 计算 bug） | 断言 `WorkspacePathBreadcrumbs` 对齐用最后一个 crumb 的 `getBoundingClientRect` | 右对齐后 `.go` 等后缀被裁 | R2 |
| CLAIM.WORK.007 | 搜索框为无背景描边胶囊：聚焦/悬停前后边框不变、高度压缩 | computed style 断言 `background: transparent`、`border-radius: 999px`、无 hover/focus border-color 变化 | 搜索框有背景填充，或聚焦后边框变色 | R0 |
| CLAIM.DOCK.005 | 右侧面板宽度/侧边栏宽度拖拽期间不触发 React 全量重渲染（CSS 变量直接驱动布局） | 断言两个 resize 的 `createRafResizeUpdater` 无 `onApply` setState | 拖拽期间每帧 setState 重渲染 App | R2 |

---

## 四、风险分级与验证方式

| 级别 | 覆盖 claim | 验证手段 |
|---|---|---|
| R0（视觉/文案） | DOCK.003, DOCK.004, WORK.007 | 静态 CSS 断言 + playwright computed style |
| R1（局部交互） | DOCK.001, DOCK.002, WORK.001, WORK.002, WORK.004, WORK.005 | store 单测 + playwright 布局断言 + Go 单测 |
| R2（跨端/持久化/搜索正确性） | WORK.003, WORK.006, DOCK.005 | 独立脚本 + 浏览器右对齐验证 + 拖拽渲染计数 |

## 五、已知边界与未覆盖

- **搜索性能**：隐藏目录全量 walk 受 `maxWalkEntries`（10000）限制，超大仓库可能截断——属设计上限，登记为可接受边界。
- **语义正确性**：预览内容渲染（代码高亮等）的正确性不在此能力描述范围（属预览渲染组件自身能力）。
- **多显示器/缩放**：折叠按钮定位在极端缩放比下的像素级对齐未全覆盖，人工抽查。
- **改动视图业务**：`changed` 视图的 git 业务逻辑（diff/回退）不在本描述范围（仅其标签页外壳）。
