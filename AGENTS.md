# AGENTS.md

本项目面向 AI 编码代理的项目指令文件。加载优先级高于构建/Git 等通用约定，低于用户当前明确指令。

## 自动推送与 PR 规则（必须遵守）

开发完成后，**构建与验证（测试等）全部通过**，无需等待额外用户确认，应**主动**将改动推送到 GitHub 并创建或更新 Pull Request（PR）。

- **触发时机**：一个功能分支的开发完成、`go build`/前端构建/测试等本地验证全部通过后，自动进入推送流程。
- **推送目标**：当前所在 feature 分支（如 `feat/*`）推送至 `origin`；若改动直接发生在 `main-v2`，则推送到 `origin/main-v2`。
- **PR 目标**：PR 统一指向 fork 仓库的默认分支 **`main-v2`**（除非用户明确指定其他 base）。
- **PR 内容**：包含变更摘要、验证命令与结果、可能的 Cache-impact/System-prompt-review 标注（如涉及缓存敏感路径）。
- **已存在 PR**：若同名 feature 分支已有 PR，则推送新提交后**更新**该 PR（amend 或追加 commit 皆可，保持 PR 单一）。

### 触发方式（AI/用户均可）

| 方式 | 命令 |
| --- | --- |
| 自动（构建验证通过） | AI 完成验证后直接执行下方推送+PR 命令 |
| 手动（用户触发） | 用户说"推送/建 PR/同步"或提及 P2/T2 规则即触发 |
| 斜杠命令 | 在 REASONIX 内运行 `/agents` 或提及"PR 规则" |

### 推送 + 创建/更新 PR 命令

```bash
# 1) 提交（若有未提交改动）
git add -A
git commit -m "feat: <描述>"    # 参照仓库提交风格

# 2) 推送当前分支
git push -u origin HEAD

# 3) 创建/更新 PR（需 gh CLI 或等效工具；没有则用 web 端）
gh pr create --base main-v2 --head <当前分支> --title "<PR 标题>" --body "<摘要+验证结果>"
# 若 PR 已存在：
gh pr view <当前分支> --json url   # 查已有 PR
```

## 本地与共享配置分层

| 文件 | 是否提交到 GitHub | 用途 |
| --- | --- | --- |
| `AGENTS.md`（本文件） | ✅ 提交 | 共享给所有协作者/AI 的项目规则 |
| `AGENTS.local.md` | ❌ 仅本地（被 `.gitignore` 过滤） | 个人本地 AI 指令，不随仓库分发 |
| `REASONIX.md` | ✅ 已由仓库维护 | Reasonix 引擎专用指令 |

## 文档引用约定

项目正式文档（`docs/`）如需引用本文件，统一用相对链接（见 `docs/AGENTS_REFERENCE.md` 的约定与示例）。

## 验证

- 推送前运行：`gofmt -w . && go vet ./... && go test ./internal/tool/builtin/ ./internal/boot/`（仓库既有 CI 前置规则）。
- PR 创建后确认：`gh pr view --json url` 或 GitHub 网页可见。