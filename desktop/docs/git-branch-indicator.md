# Git 分支指示器 — 方案说明

## 需求

桌面客户端的右侧面板「改动」视图中，没有显示当前 Git 分支名称。用户希望在工作区改动视图的搜索框上方看到当前分支。

## 实现方案

### 数据流

```
App.WorkspaceChanges()           ← Go 后端绑定方法
  ├─ workspaceGitBranch(base)    ← 新增：执行 git branch --show-current
  ├─ workspaceGitStatus(base)    ← 已有
  └─ WorkspaceChangesView        ← 扩展：新增 GitBranch 字段
       → 前端 WorkspacePanel     ← 渲染分支指示器
```

### 改动文件

| 文件 | 改动 |
|---|---|
| `desktop/workspace_changes.go` | 新增 `workspaceGitBranch()` 函数；在 `WorkspaceChanges()` 中调用并赋值；detached HEAD 时回退显示短 commit |
| `desktop/app.go` | `WorkspaceChangesView` 结构体添加 `GitBranch string` 字段 |
| `desktop/frontend/src/lib/types.ts` | `WorkspaceChangesView` 接口添加 `gitBranch?: string` |
| `desktop/frontend/src/components/WorkspacePanel.tsx` | 搜索框上方插入分支指示器 JSX |
| `desktop/frontend/src/styles.css` | 新增 `.workspace-branch-indicator` 样式 |

### 渲染位置

右侧面板 → 改动 tab → 在 `workspace-files__tools`（标签切换栏）和 `workspace-search`（搜索框）之间插入分支指示器。

仅在 `viewMode === "changed"` 且 `changes.gitBranch` 非空时显示。普通分支显示分支名；detached HEAD 显示 `@<short-sha>`。

### 效果

```
[GitBranch icon] main
┌─────────────────────┐
│ 🔍 Search files…   │
└─────────────────────┘
<改动文件列表>
```

## 验证

- `go test . -run 'TestWorkspaceChanges|TestParseGitStatusPorcelainZ'`
- `npm run typecheck`
- `npm run check:css`
