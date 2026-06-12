# Desktop 会话持久化分析与常见问题

> 分析目的：解释 Desktop（`reasonix-desktop`）中会话保存和恢复的机制，诊断"重启 Desktop 后对话丢失"的根因。  
> 基于 commit `654fdc45` | 用户启动方式：`my-scripts/start-desktop.sh`

---

## 一、会话保存链路

### 1.1 Desktop 的存储目录

Desktop 确定会话存储目录的函数链：

```
TUI (reasonix chat):
  config.SessionDir() → os.UserConfigDir()/reasonix/sessions/
  → ~/Library/Application Support/reasonix/sessions/

Desktop global tab:
  desktopSessionDir(globalWorkspaceRoot()) 
  → ProjectSessionDir(globalWorkspaceRoot)     → 项目级目录
  → 失败时回退 config.SessionDir()              → 全局目录

Desktop project tab:
  desktopSessionDir(workspaceRoot)
  → ProjectSessionDir(workspaceRoot)
  → ~/Library/Application Support/reasonix/projects/<workspace-slug>/sessions/
```

关键观察：**Desktop 的 Global tab 也走 `ProjectSessionDir`，不会直接使用 `config.SessionDir()`**。

### 1.2 目录解析链

```go
// desktop/sessions.go:38-51
func desktopSessionDir(root string) string {
    root = strings.TrimSpace(root)
    if root == "" {
        cwd, _ := os.Getwd()
        root = cwd
    }
    if dir := config.ProjectSessionDir(root); dir != "" {
        return dir            // ← 优先走项目级目录
    }
    return config.SessionDir()  // ← 回退全局目录
}

// internal/config/config.go:1787-1797
func ProjectSessionDir(workspaceRoot string) string {
    base := MemoryUserDir()
    return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

func globalWorkspaceRoot() string {
    return filepath.Join(os.UserConfigDir(), "reasonix", "global-workspace")
}
```

Global tab 的会话目录最终为：
```
~/Library/Application Support/reasonix/projects/
  -Users-weikejia-Library-Application Support-reasonix-global-workspace/
    sessions/
```

### 1.3 Tab 保存链路

```
桌面关闭 / 崩溃:
  beforeClose → shutdown → for each tab: Ctrl.Snapshot() + Ctrl.Close()
  Ctrl.Snapshot() → s.Save(sessionPath) → JSONL 全量重写

运行时自动保存:
  turn结尾 → snapshotActivityIfChanged → s.Save(sessionPath)  [每轮]
  tabSnapshotLoop → tab.Ctrl.Snapshot()                       [每30秒]

Tab 元数据持久化:
  buildTabController → persistTabSessionPath(tab, path) 
  → rememberTabSessionPath(tab, path) 
  → tab.SessionPath = path + saveTabsLocked()
  → ~/.config/reasonix/desktop-tabs.json
```

### 1.4 启动恢复链路

```
Desktop 启动:
  loadTabsFile() → ~/.config/reasonix/desktop-tabs.json
  → 恢复所有 tab（含 SessionPath/TopicID/Model 等字段）
  → 对每个 tab 启动 buildTabController (go routine, 异步)

buildTabController:
  1. sessionDir = desktopSessionDir(root)      ← 计算存储目录
  2. boot.Build(SessionDir: sessionDir)         ← 创建 Controller
  3. loadPinnedTabSession(dir, tab.SessionPath)  ← 尝试恢复会话（关键步骤）
  4. 如果 tab.SessionPath 为空 / 文件不存在:
      → findTopicSession(dir, tab.TopicID)     ← 按主题找
      → 都失败 → agent.NewSessionPath(dir)     ← 新建
  5. persistTabSessionPath(tab, path)           ← 更新 desktop-tabs.json
```

---

## 二、根因分析

### 2.1 关键函数：`validateSessionPath` 的路径隔离

```go
// desktop/sessions.go:332-354
func validateSessionPath(dir, sessionPath string) (string, string, error) {
    absDir, _ := filepath.Abs(dir)
    absPath, _ := filepath.Abs(sessionPath)
    rel, err := filepath.Rel(absDir, absPath)
    if err != nil || rel == "." || 
       strings.HasPrefix(rel, ".."+string(filepath.Separator)) || 
       rel == ".." || filepath.IsAbs(rel) {
        return "", "", fmt.Errorf("session path outside session dir: %s", sessionPath)
    }
    // ...
}
```

**隔离规则**：`sessionPath` 必须解析为 `dir` 的相对路径，且不能包含 `..` 跳出 `dir`。

### 2.2 根因：`start-desktop.sh` 导致的目录跨越

`start-desktop.sh` 的执行流程：

```bash
# my-scripts/start-desktop.sh
DESKTOP_DIR="$PROJECT_DIR/desktop"
cd "$DESKTOP_DIR"           # ← 切换 cwd
exec wails dev               # ← 从 desktop/ 目录启动
```

当 Desktop 启动时，**cwd 为 `.../desktop/`**。

**核心矛盾**：

```
Desktop 的会话目录:
  ProjectSessionDir(globalWorkspaceRoot())
  = ~/Library/Application Support/reasonix/projects/
    -Users-weikejia-Library-Application Support-reasonix-global-workspace/
      sessions/

TUI 的会话目录:
  config.SessionDir()
  = ~/Library/Application Support/reasonix/sessions/

→ 两个目录完全不同！
→ Desktop 看不到 TUI 的历史会话文件
```

### 2.3 根因细节：历史面板加载会话后的重启丢失

```
1. 用户之前有 TUI 会话  →  会话存在 config.SessionDir()

2. 用户运行 start-desktop.sh 启动 Desktop
   → Desktop 创建默认 Global tab（无历史）
   → 用户打开 Desktop 的"历史会话"面板
   → 面板列出 config.SessionDir() 中的会话文件
   → 用户点击了之前的 TUI 会话

3. Desktop 为该会话创建新 Tab:
   → tab.SessionPath = "~/.../reasonix/sessions/xxx.jsonl"  (全局目录)
   → persistTabSessionPath → 保存到 desktop-tabs.json

4. 用户追问 2 次 → Ctrl.Snapshot() → 写入全局目录的 JSONL（成功）

5. 用户重启 Desktop:
   → buildTabController:
       dir = desktopSessionDir(globalWorkspaceRoot())
          ↓  ProjectSessionDir(globalWorkspaceRoot)
          ↓  "~/.../reasonix/projects/<slug>/sessions/"  ← 项目级目录
   → loadPinnedTabSession(项目级目录, 全局目录下的 xxx.jsonl)
       → validateSessionPath(项目级目录, 全局目录下的路径)
       → filepath.Rel(项目级目录, 全局目录路径)
       → 返回 "../../sessions/xxx.jsonl"
       → 包含 ".." → REL 检查失败!
       → 会话恢复失败!  ← 这就是丢失的确切原因
```

---

## 三、验证清单

```bash
# 1. desktop-tabs.json 中 tab 的 sessionPath 指向什么？
cat ~/Library/Application\ Support/reasonix/desktop-tabs.json

# 2. 验证 sessionPath 是否在 sessionDir 之下
#    realpath --relative-to=<sessionDir> <sessionPath>
#    → 如果包含 ".." 开头，重启后会恢复失败

# 3. 对比 TUI 和 Desktop 的会话目录
echo "TUI:    ~/Library/Application Support/reasonix/sessions/"
echo "Desktop: ~/Library/Application Support/reasonix/projects/"
```

---

## 四、解决方案

### 4.1 短期恢复

找到原始 JSONL 文件（在 `config.SessionDir()` 中），在 Desktop 运行时通过历史面板手动加载。

### 4.2 避免再次发生

不要在 Desktop 历史面板中"打开"TUI 会话，而是新建空 Tab 开始新对话。

### 4.3 代码级改进

| 位置 | 问题 | 建议修复 |
|:-----|:-----|:---------|
| `desktop/sessions.go:38-51` | Global tab 使用 `ProjectSessionDir()` 而非 `config.SessionDir()` | Global tab 直接存储到 `config.SessionDir()` |
| `desktop/sessions.go:348-353` | `validateSessionPath` 拒绝含 `..` 的路径 | 允许 `config.SessionDir()` 作为白名单目录 |

---

*报告生成日期：基于 main-v2 branch（commit `654fdc45`）*
