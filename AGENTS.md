# AGENTS.md — DeepSeek-Reasonix 编译指南与自定义功能说明

## 项目信息

| 项 | 值 |
|---|---|
| 仓库 | `https://github.com/esengine/DeepSeek-Reasonix` |
| 分支 | `main-v2` |
| Fork | `https://github.com/expfukck/DeepSeek-Reasonix` |
| 功能分支 | `feat/encoding-support-and-rewind-fix` |
| PR | `https://github.com/esengine/DeepSeek-Reasonix/pull/3108` |
| 本地路径 | `C:\Users\Administrator\DeepSeek-Reasonix` |
| 编译产物 | `desktop\build\bin\reasonix-desktop.exe` (~16 MB) |
| 桌面框架 | Wails v2.12.0 |
| 前端包管理 | pnpm |

---

## 一键编译（推荐）

```powershell
cd C:\Users\Administrator\DeepSeek-Reasonix
python update_build.py
```

脚本自动完成：
1. `git fetch` + `git reset --hard origin/main-v2` 拉取最新源码
2. `wails build` 编译 desktop production build

脚本会自动杀掉可能正在运行的旧进程，避免权限错误。

---

## 手动流程（备用）

### 1. 拉取最新源码

```powershell
cd C:\Users\Administrator\DeepSeek-Reasonix
git fetch origin main-v2
git reset --hard origin/main-v2
```

### 2. 编译

```powershell
cd C:\Users\Administrator\DeepSeek-Reasonix\desktop
wails build
```

产物路径：`desktop\build\bin\reasonix-desktop.exe`

---

## 环境依赖

| 工具 | 用途 |
|------|------|
| `git` | 拉取源码 |
| `go` | Go 编译器 |
| `wails` (v2.12.0) | 桌面应用编译 |
| `pnpm` | 前端依赖安装 |
| `gh` (GitHub CLI) | Fork 同步 / PR 管理 |

---

## 注意事项

1. **`git reset --hard` 会清除所有本地修改** — 确保重要改动已提交或备份
2. **编译时间**：首次约 30-60 秒（需安装前端依赖），后续约 8-10 秒
3. **产物大小**：约 16 MB

---

## 自定义功能分支（Fork + Rebase 工作流）

### 目录结构

```
C:\Users\Administrator\DeepSeek-Reasonix\                    ← 原项目（日常使用）
  ├── desktop\build\bin\reasonix-desktop.exe                 ← 编译产物
  └── AGENTS.md                                              ← 本文件

C:\Users\Administrator\DeepSeek-Reasonix\DeepSeek-Reasonix\  ← Fork 本地克隆（开发用）
  └── 分支: feat/encoding-support-and-rewind-fix (6 commits)
```

### PR 包含的全部改动（6 commits）

| # | Commit | 改动摘要 |
|---|--------|----------|
| 1 | `36cca86a` feat(workspace): add file encoding selector in file browser | 桌面文件浏览器编码选择器 UI |
| 2 | `9864c141` feat(tools): add encoding parameter to file tools and fix rewind bug | 工具层编码参数 + 回滚 Bug 修复 |
| 3 | `3575fae1` feat: project-level file encoding setting (reasonix.toml) | 项目级编码配置 |
| 4 | `9b443cac` fix(config): render file_encoding in RenderTOML so it persists to disk | 修复 TOML 渲染遗漏字段 |
| 5 | `dd5379df` fix(ui): move encoding selector to file browser toolbar | 编码选择器移到工具栏 |
| 6 | `fff94377` fix(edit): improve error diagnostics and fix Preview CRLF/encoding bugs | 编辑诊断增强 + Preview 修复 |

---

### 改动详细说明

#### 1. 项目级编码设置（reasonix.toml）

在 `reasonix.toml` 中设置 `file_encoding`，全局影响所有文件操作：

```toml
file_encoding = "GB18030"   # 项目文件编码；留空 = 逐文件自动检测
```

- **read_file**：用项目编码作为默认解码方式
- **write_file**：新建文件用项目编码写入；覆写已有文件自动保留原编码
- **edit_file / multi_edit**：读写都用项目编码
- **文件浏览器**：工具栏下拉菜单切换编码，即时生效并写入配置
- 支持的编码：`UTF-8`, `UTF-8 BOM`, `GB18030`(兼容 GBK/GB2312), `UTF-16 LE`, `UTF-16 BE`, `UTF-16 LE (no BOM)`, `UTF-16 BE (no BOM)`

**传递链**：`reasonix.toml` → `config.Config.FileEncoding` → `boot.Build()` → `Workspace{FileEncoding}` → 各工具 struct `{fileEncoding}` → Execute/Preview

#### 2. 回滚 Bug 修复

**根因**：回滚后旧 checkpoint 未清理，新的 turn 编号与旧快照冲突，导致 `RestoreCode` 取到错误的（更旧的）快照，文件无法恢复。

**修复**：
- `checkpoint.Store` 新增 `Prune(fromTurn)` 方法 — 回滚后删除 `Turn >= fromTurn` 的所有检查点（内存 + 磁盘 JSON）
- `Controller.Rewind()` 中调用 `Prune` 防止 turn 编号冲突
- 前端 Rewind / Fork / Summarize 错误不再静默吞掉，通过 `local_notice` 显示给用户

#### 3. 编辑诊断增强（P0 + P1）

**P0 — Preview 正确性修复**：
- `editFile.Preview` 和 `multiEdit.Preview` 补上 `matchLineEndings()` 调用（之前缺少导致 CRLF 文件审批预览报 "not found"）
- Preview 方法补上 `fileEncoding` 回退（之前只用 per-call 参数，和 Execute 行为不一致）

**P1 — 智能错误诊断**：

之前：
```
old_string not found in main.go
old_string is not unique; add more surrounding context
```

之后：
```
old_string not found in main.go. Nearest match at line 42:
  expected: `func processRequest(`
  actual:   `func ProcessRequest(`

old_string is not unique in main.go (3 matches at line 12, line 42, line 78);
add more surrounding context to disambiguate
```

共享诊断函数（`encoding_helpers.go`）：
- `diagnoseNotFound()` — 定位最近匹配行 + 显示 expected vs actual 差异
- `diagnoseNotUnique()` — 显示匹配数量 + 每个匹配的行号
- `matchLineNumbers()` / `commonPrefixLen()` / `formatLineList()` — 辅助函数

#### 4. 改动文件清单（共 ~20 个文件）

| 层 | 文件 | 改动类型 |
|---|------|----------|
| 编码核心 | `internal/fileutil/encoding/encoding.go` | 新增 `Name()` / `ParseName()` |
| 工具实现 | `readfile.go`, `writefile.go`, `editfile.go`, `multiedit.go` | struct 加 `fileEncoding`，Execute 加诊断 |
| 工具预览 | `preview.go` | 修复 matchLineEndings + fileEncoding + 诊断 |
| 工具辅助 | `encoding_helpers.go` | 新增 `readFileEncodedWith` / `writeFileEncodedWith` / 诊断函数 |
| 工具传递 | `workspace.go`, `confine.go` | 传递 `fileEncoding` |
| 检查点 | `internal/checkpoint/checkpoint.go` | 新增 `Prune()` |
| 控制器 | `internal/control/controller.go` | Rewind 调用 Prune |
| 启动 | `internal/boot/boot.go` | 传递 `fileEncoding` |
| 配置 | `internal/config/config.go`, `render.go` | 新增 `FileEncoding` 字段 + TOML 渲染 |
| 桌面后端 | `desktop/app.go`, `settings_app.go` | ReadFile 编码参数 + SetFileEncoding |
| 桌面前端 | `WorkspacePanel.tsx`, `bridge.ts`, `types.ts`, `useController.ts` | 编码选择器 + 错误显示 |
| 样式 | `styles.css` | 编码选择器样式 |
| 国际化 | `en.ts`, `zh.ts` | 编码相关中英文字符串 |

---

### 同步上游最新代码（不合并 PR 也能持续使用）

当上游 `main-v2` 有新提交时，执行以下步骤将改动 rebase 到最新代码上：

```powershell
cd C:\Users\Administrator\DeepSeek-Reasonix\DeepSeek-Reasonix

# 1. 同步 fork 与上游
gh repo sync expfukck/DeepSeek-Reasonix --source esengine/DeepSeek-Reasonix --branch main-v2

# 2. 拉取最新
git fetch fork

# 3. 将你的 commit rebase 到最新 main-v2 上
git rebase fork/main-v2

# 4. 验证编译
go vet ./...
go test ./internal/checkpoint/... ./internal/tool/builtin/... ./internal/fileutil/encoding/...

# 5. 编译桌面版
cd desktop
wails build

# 6. 替换 exe（先关闭正在运行的应用）
Copy-Item "build\bin\reasonix-desktop.exe" "..\..\desktop\build\bin\reasonix-desktop.exe" -Force

# 7. 更新 PR 分支（可选）
cd ..
git push fork feat/encoding-support-and-rewind-fix --force
```

### 冲突处理

大部分时候 rebase 零冲突。若出现冲突：

```powershell
# 打开冲突文件手动解决后：
git add .
git rebase --continue
git push fork feat/encoding-support-and-rewind-fix --force
```

### 一键同步脚本（可选）

如需将上述流程自动化，可在 `update_build.py` 基础上增加 rebase 逻辑，或单独写一个 `sync_custom.ps1`。
