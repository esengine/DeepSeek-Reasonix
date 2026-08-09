# 引用 AGENTS.md 的约定

> 本文档说明：项目正式文档（`docs/` 目录）中如何引用根目录的 `AGENTS.md` 指令文件。

## 引用写法

统一使用**相对链接**（`.md` 后缀，相对于 `docs/` 目录）：

```markdown
[项目 AI 指令（AGENTS.md）](../AGENTS.md)
```

在文档正文中引用的示例写法：

```markdown
本项目面向 AI 编码代理的关键工作约定见 [AGENTS.md](../AGENTS.md)，
其中「自动推送与 PR 规则」要求在构建与验证全部通过后主动推送并创建/更新 PR。
```

## 放置位置 / 约定目录

- **AGENTS.md 本体**：固定在**项目根目录**（`/AGENTS.md`），是所有 AI 代理（Reasonix、Claude Code、Codex 等）与协作者统一加载的指令文件。
- **docs/ 下的引用**：建议集中放在 `docs/AGENTS_REFERENCE.md`（即本文件）作为索引；其他文档需要时用上述相对链接指向它即可，避免重复粘贴规则正文（保持单一事实来源）。
- **本地变体**：个人本地指令放 `AGENTS.local.md`（根目录，已被 `.gitignore` 过滤，**不随仓库同步**）——见 AGENTS.md 中的「本地与共享配置分层」。

## 为什么不用绝对路径或空链接

- 文档会随仓库克隆到不同路径，相对链接保证在 GitHub 网页与本地都能跳转。
- 不引用 `blob/<hash>` 或特定提交，避免链接随提交失效。

## 目录结构示例

```
DeepSeek-Reasonix/
├── AGENTS.md                 <- 共享规则（提交）
├── AGENTS.local.md           <- 本地个人规则（不提交，gitignore）
├── .gitignore                <- 过滤 AGENTS.local.md
└── docs/
    └── AGENTS_REFERENCE.md   <- 本文件（引用索引）
```