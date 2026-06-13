# Comet 工作流集成

## 一句话说明

为 Reasonix 接入 Comet 的五阶段 AI 编码工作流（open → design → build → verify → archive），让 Agent 在 spec-driven 的框架下有结构地完成从"想法到归档"的完整开发过程。

## 解决什么问题

当前 Reasonix 的 Agent 在长期开发任务中存在三个痛点：

| 痛点 | 表现 |
|------|------|
| **中断无法续接** | 关闭会话后重开，Agent 不知道上次做到哪了，需要重新读代码、猜进度 |
| **跳过设计直接写码** | Agent 倾向于跳过 brainstorming 和设计方案，直接动手写代码 |
| **阶段无校验** | Agent 说"做完了"但实际任务未完成、spec 未同步、验证未通过 |

Comet 通过 **状态机 + 门禁脚本 + 阶段技能** 三层机制解决这三个问题。

## 工作流概览

```
/comet <描述>
    ↓
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ 1. Open      │ ──→ │ 2. Design     │ ──→ │ 3. Build      │ ──→ │ 4. Verify     │ ──→ │ 5. Archive    │
│ 提案+设计+任务│     │ 深入设计+头脑风│     │ 计划+编码实现  │     │ 验证+分支处理  │     │ Spec合并+归档 │
│ (OpenSpec)   │     │ (Superpowers) │     │ (Superpowers) │     │ (Both)        │     │ (OpenSpec)    │
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
```

## 核心机制

### 1. 状态机（`.comet.yaml`）

每次阶段切换时写入当前阶段，中断后 `/comet` 自动读取状态并续接：

```yaml
workflow: full
phase: build          # ← 当前所在阶段
build_mode: subagent-driven-development
isolation: branch
verify_result: pending
```

### 2. 门禁校验（Guard Scripts）

阶段转换前，shell 脚本强制检查退出条件，不合格则阻挡：

```
$ comet-guard.sh <change> build --apply
  [PASS] isolation selected
  [PASS] build_mode selected
  [FAIL] tasks.md all tasks checked    ← 还有未完成任务，阻止进入 verify
```

### 3. 阶段技能（Phase Skills）

每个阶段对应一个 Agent 技能（`/comet-open`、`/comet-design` 等），提供详细的分步指导。

## 技术架构

```
┌─────────────────────────────────────────────────────┐
│                    Reasonix 内核                      │
│  ┌──────────┐  ┌──────────┐  ┌───────────────────┐  │
│  │ Hook 系统 │  │ Task 子代理│  │ Memory/Checkpoint │  │
│  └──────────┘  └──────────┘  └───────────────────┘  │
│        ↑ 未来集成点                                   │
├─────────────────────────────────────────────────────┤
│                   Comet 集成层                        │
│  ┌──────────────────┐  ┌──────────────────────────┐ │
│  │ 8 个 SKILL.md     │  │ 7 个 Shell 脚本           │ │
│  │ (Prompt 驱动指令)  │  │ (状态机 + 门禁 + 归档)    │ │
│  └──────────────────┘  └──────────────────────────┘ │
│                         ┌──────────────────────────┐ │
│                         │ OpenSpec CLI (npm)        │ │
│                         │ Superpowers 技能 (14 个)   │ │
│                         └──────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

## 当前实现状态

本分支已完成 **Level 1：技能文件部署**，即 Comet + Superpowers + OpenSpec 三层技能文件均已部署到项目 `.reasonix/skills/`，Agent 可直接使用 `/comet` 触发完整工作流。

| 组件 | 数量 | 状态 |
|------|------|------|
| Comet 阶段技能 | 8 | ✅ 已部署 |
| Comet Shell 脚本 | 7 | ✅ 已部署 |
| Superpowers 技能 | 14 | ✅ 已部署 |
| OpenSpec 桥接技能 | 5 | ✅ 已部署 |
| `openspec` CLI | v1.4.1 | ✅ 已安装 |

## 后续计划（不在本次 PR 范围）

| 阶段 | 内容 | 说明 |
|------|------|------|
| **Level 2** | Go CLI: `reasonix comet init` | 一条命令完成全部安装，替代手动部署 |
| **Level 3** | Go 化门禁逻辑 | 将 guard/state 脚本重写为 Go，纳入 `internal/workflow/` |
| **Level 4** | Hook 集成 | Guard 脚本转为 Reasonix 原生 PreToolUse 钩子 |
| **Level 5** | Task 子代理对接 | Build 阶段 subagent-driven-development 对接 `TaskTool` |
