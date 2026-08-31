# 右侧边栏改造方案

## 现状分析

### 当前右侧面板结构（workbench-dock）

当前右 dock 是一个**固定宽度的侧边栏面板**，包含：

**Tab 栏（workbench-dock__tabs）：**
- 概览（context）— 会话概览/用量
- 文件（files）— 工作区文件树
- 变更（changed）— Git 变更视图
- 远程（remote）— 远程服务器列表（仅在有远程主机时显示）
- 指令（instructions）— 项目指令

**内容区（workbench-dock__body）：**
- 根据 tab 切换显示：ContextPanel / RemotePanel / InstructionPanel / WorkspacePanel
- 终端（TerminalPanel）是独立于 dock 的底部抽屉（terminal-drawer）

**问题：**
- 所有功能入口都挤在水平 tab 条里，类别有限且无法扩展
- 终端是独立抽屉，与右侧面板分离
- 没有"浏览器/网页预览"等现代 IDE 工具面板
- 没有"+"添加新 tab 的能力

### 参考：Codex / VS Code 活动栏模式

Codex（GitHub 的 AI 编辑器）和 VS Code 采用**活动栏（Activity Bar）→ 侧边栏（Side Bar）→ 面板（Panel）**三层结构：

1. **活动栏（窄竖条）**：固定图标入口，每个图标代表一个工具（文件、搜索、Git、调试、扩展）
2. **侧边栏**：点击活动栏图标后展开，显示对应工具的详细视图
3. **面板（底部）**：终端、输出、问题等

核心差异：活动栏始终可见，点击后展开侧边栏；侧边栏内可有多 Tab。

## 改造目标

将当前右侧面板改造为 **Codex 风格的活动栏 + 多 Tab 容器**：

```
┌────────────────────────────────────────────┐
│  活动栏（窄竖条）    │  Tab 容器（展开时显示）    │
│  ┌──────────────┐   │  ┌──────────────────┐   │
│  │  📁 文件      │   │  │  Tab栏 [+ 添加]  │   │
│  │  🌿 变更      │   │  │──────────────────│   │
│  │  🔌 远程      │   │  │  内容区域         │   │
│  │  📖 指令      │   │  │  (网页/文件/      │   │
│  │  💻 终端      │   │  │   终端/远程等)    │   │
│  │  🌐 浏览器    │   │  │                  │   │
│  │  📋 概览      │   │  │                  │   │
│  └──────────────┘   │  └──────────────────┘   │
└────────────────────────────────────────────┘
```

### 交互逻辑

1. **默认状态**：右侧显示一条窄竖条活动栏（约 48px），显示工具入口图标
2. **点击入口**：展开全局 Tab 容器，之前的活动栏入口图标消失
3. **Tab 容器结构**：
   - 顶部：Tab 栏（显示当前打开的 Tab 标签），右侧有 **+ 按钮**
   - 中间：当前 Tab 的内容区域
4. **+ 按钮**：点击弹出菜单，可选择添加：终端、浏览器、文件、远程等新 Tab
5. **关闭 Tab**：Tab 有关闭按钮（×），关闭最后一个 Tab 时容器收起，活动栏重新显示

## 详细设计

### 1. 组件结构

```
components/
├── ActivityBar/
│   ├── ActivityBar.tsx          # 活动栏容器
│   ├── ActivityBarEntry.tsx     # 单个入口图标
│   └── activityBarConfig.ts     # 入口配置列表
├── TabContainer/
│   ├── TabContainer.tsx         # Tab 容器（含 tab bar + 内容区）
│   ├── TabBar.tsx               # Tab 栏（tab 标签 + 关闭按钮）
│   ├── TabAddMenu.tsx           # + 添加菜单
│   └── TabContent.tsx           # Tab 内容渲染器
├── BrowserPanel.tsx             # 浏览器/网页预览面板（新增）
└── ...现有面板复用
```

### 2. 状态管理

```typescript
// 新增 store: activityBar.ts
interface ActivityBarState {
  // 活动栏
  activityBarOpen: boolean;          // 是否展开
  activeEntry: string | null;        // 当前激活的入口 ID
  // Tab 容器
  tabs: TabItem[];                   // 已打开的 tab 列表
  activeTabId: string | null;        // 当前 tab ID
  // 宽度
  tabContainerWidth: number;         // 展开后的容器宽度
}

interface TabItem {
  id: string;
  type: TabType;                     // 'file' | 'terminal' | 'browser' | 'remote' | 'context' | 'instructions' | 'workspace'
  label: string;
  icon?: ReactNode;
  meta?: Record<string, unknown>;    // 各类型特有数据，如 URL、文件路径等
}

type TabType = 'file' | 'changed' | 'terminal' | 'browser' | 'remote' | 'context' | 'instructions';
```

### 3. 活动栏入口配置

```typescript
const ACTIVITY_BAR_ENTRIES = [
  { id: 'files',       icon: FileText,    label: '文件',     defaultTab: 'file' },
  { id: 'changed',     icon: GitBranch,   label: '变更',     defaultTab: 'changed' },
  { id: 'remote',      icon: Server,      label: '远程',     defaultTab: 'remote' },
  { id: 'instructions', icon: BookOpen,   label: '指令',     defaultTab: 'instructions' },
  { id: 'terminal',    icon: Terminal,    label: '终端',     defaultTab: 'terminal' },
  { id: 'browser',     icon: Globe,       label: '浏览器',   defaultTab: 'browser' },  // 新增
  { id: 'context',     icon: Activity,    label: '概览',     defaultTab: 'context' },
];
```

### 4. 交互流程

```
用户点击活动栏图标
  → 计算点击位置，展开 Tab 容器（从右侧滑入动画）
  → 如果该类型已有打开的 Tab，切换到该 Tab
  → 如果没有，创建默认 Tab 并打开
  → 活动栏入口图标隐藏（活动栏保持可见但该入口变灰/隐藏）

用户点击 Tab 栏的 + 按钮
  → 弹出菜单（AddMenu），列出可添加的 Tab 类型
  → 选择类型后创建新 Tab 并切换到它

用户点击 Tab 的 × 关闭按钮
  → 关闭该 Tab
  → 如果只剩一个 Tab 且关闭，收起容器，显示活动栏
  → 切换到相邻 Tab

用户拖拽 Tab 排序（可选，后续迭代）
```

### 5. 新增组件：浏览器面板

需要新增 `BrowserPanel` 组件：
- 使用 `<webview>` 或 `<iframe>` 嵌入网页
- 顶部有地址栏 + 前进/后退/刷新按钮
- 支持导航到任意 URL（如 AI 生成的页面预览、文档等）

### 6. 布局适配

当前布局：`App.tsx` 中 `workbench-dock` 区域

改造后布局变动：
```
<!-- 活动栏 -->
<aside className="activity-bar">
  {ACTIVITY_BAR_ENTRIES.map(entry => (
    <button key={entry.id} onClick={() => toggleActivityEntry(entry.id)}>
      <entry.icon />
      <span>{entry.label}</span>
    </button>
  ))}
</aside>

<!-- Tab 容器（展开时） -->
{activityBarOpen && (
  <aside className="tab-container">
    <TabBar tabs={tabs} activeTabId={activeTabId} onAdd={showAddMenu} />
    <div className="tab-container__body">
      {renderActiveTabContent()}
    </div>
  </aside>
)}
```

### 7. 面板复用策略

| 现有面板 | 处理方式 |
|---------|---------|
| WorkspacePanel (files/changed) | 作为 Tab 内容复用，去掉内部 tab 切换 |
| ContextPanel | 作为 Tab 内容复用 |
| InstructionPanel | 作为 Tab 内容复用 |
| RemotePanel | 作为 Tab 内容复用 |
| TerminalPanel | 从底部抽屉移到 Tab 容器，但保持底部可选 |
| BrowserPanel | 新增 |

## 实施计划

### Phase 1：基础框架（核心改动）

1. **创建 `ActivityBar` 组件** — 竖条容器 + 入口图标
2. **创建 `TabContainer` / `TabBar` 组件** — Tab 栏 + 内容区
3. **创建 `activityBarStore`** — 状态管理（Zustand）
4. **修改 `App.tsx` 布局** — 替换现有 `workbench-dock` 为活动栏 + Tab 容器
5. **实现 + 添加菜单** — `TabAddMenu` 组件

### Phase 2：面板迁移

6. **WorkspacePanel 适配** — 作为 Tab 内容渲染
7. **ContextPanel / InstructionPanel / RemotePanel 迁移** — 作为 Tab 内容
8. **TerminalPanel 适配** — 支持在 Tab 容器中打开

### Phase 3：浏览器面板

9. **创建 `BrowserPanel` 组件** — 嵌入网页预览
10. **地址栏 + 导航控件** — URL 输入框、前进/后退/刷新

### Phase 4：打磨

11. **动画过渡** — 活动栏展开/收起动画
12. **宽度拖拽** — Tab 容器宽度可拖拽调整
13. **Tab 拖拽排序** — 拖拽重排 Tab 顺序
14. **状态持久化** — 保存打开的 Tab 列表和容器宽度

## 关键约束

- **向后兼容**：现有 ContextPanel / RemotePanel / InstructionPanel / WorkspacePanel 的 props 接口尽量不改
- **布局稳定**：活动栏和 Tab 容器宽度变化不应导致聊天区宽度跳动（参考现有 `workspacePanelRenderWidth` 机制）
- **终端兼容**：TerminalPanel 既可在 Tab 容器中打开，也可保留底部抽屉模式
- **i18n**：所有新增文本走 locale 文件
- **CSS 变量**：所有样式使用 CSS 变量，同时支持深色/浅色主题

## 未覆盖项（后续迭代）

- Tab 拖拽排序
- 分屏/并排视图
- 活动栏条目顺序自定义
- 浏览器面板的 Cookie/登录态复用