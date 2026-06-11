# Memory System Improvement Plan

## 现状诊断

### 当前架构

```
启动时:
  memory.Load() ──→ 扫描文档 + 读取事实索引 ──→ memory.Compose() ──→ # Memory 块
                                                                         ↓
                                                                  系统 prompt（缓存）
会话中:
  用户: 记住 X
  tool: remember() ──→ 写入磁盘 ──→ QueueMemory(note) ──→ 下一轮注入 <memory-update>
```

### 5 个短板

| 短板 | 影响 |
|---|---|
| **Doc 更新不生效** | 启动后改 REASONIX.md，重启才生效 |
| **事实全文加载** | 所有事实全量编译进前缀，浪费 token |
| **无激活模式** | always-on 是唯一模式，无法按需加载 |
| **SaveDoc 注入粗暴** | 编辑 doc 后全文注入 user message |
| **索引可能过期** | forget 后 index 行仍在缓存中 |

---

## 业界方案对比

| 维度 | Claude Code | Windsurf | Cursor | Copilot | Reasonix |
|---|---|---|---|---|---|
| **指令文件** | CLAUDE.md（多级） | .devin/rules/ | .cursorrules | copilot-instructions.md | REASONIX.md（多级） |
| **自动记忆** | ✓ 事实 + topic files | ✓ Memories | ✗ | ✗ | ✓ 事实存储 |
| **加载方式** | index-only（首200行）+ 按需 read_file | 按激活模式 | 全量 | 全量 | **全量编入前缀** |
| **激活模式** | 无（always-on） | always_on / model_decision / glob / manual | 部分 glob | 无 | **无（always-on）** |
| **会话中更新** | 写入磁盘 → 下一轮可见 | 写入磁盘 → 下一轮可见 | N/A | N/A | 写入 + turn-tail 注入 |
| **前缀缓存** | 启动时加载 → 缓存 | 每次会话加载 | 每次会话加载 | 每次会话加载 | **启动时加载 → 缓存** |

**核心发现**：没有工具使用向量检索做记忆管理。所有工具的"记忆"本质上是**结构化文本注入到上下文**。差异点在于**注入方式**（全量 vs index-only vs 按需）和**激活策略**（always-on vs model_decision vs glob）。

---

## 理想架构

```
三层分级 + 激活模式 + 按需加载 + 轻量更新
```

### 第一层：指令文档（Docs）

**不变**：REASONIX.md / AGENTS.md / CLAUDE.md 的多级扫描 + `@import` 递归解析 + 前缀缓存。

**改进**：

| 改动 | 说明 |
|---|---|
| **内存缓存** | 将文档解析结果缓存在内存中，用户通过 UI/文件系统编辑后触发重加载 |
| **差异化注入** | 当文档更新时，只注入变化部分而非全文 |
| **缓存感知** | 利用 DeepSeek 自动前缀缓存的特性，确保同 session 内前缀不变化 |

### 第二层：自动记忆（Facts）— 核心重构

#### 2.1 索引-主题双层结构

```
memory/
├── MEMORY.md              ← 索引文件（始终在系统 prompt 中）
│   - [偏好 tabs](prefer-tabs.md) — 用户偏好使用空格缩进
│   - [数据库配置](db-config.md) — Redis 连接信息
│   └── 前端风格指南 (frontend-style/) ← 多文件主题
│       - [命名规范](frontend-style/naming.md)
│       └── [组件模式](frontend-style/patterns.md)
├── prefer-tabs.md          ← 单文件事实（按需加载 via read_file）
├── db-config.md
└── frontend-style/
    ├── naming.md
    └── patterns.md
```

**索引（MEMORY.md）**：始终在系统 prompt 中。每条一行：可读名 + 文件名 + 一句话描述。目标是**每条不超过 150 字符**。首 100 条进入前缀，超出部分旧条目自然溢出（行为同 Claude Code）。

**事实文件**：以 Markdown 文件存储，支持 frontmatter（同现有格式）。通过 `read_file` 按需加载，模型依据索引中的描述决定读取哪些。

**主题目录**：相关事实可以归入子目录（topic），索引中显示为可折叠条目。主题目录整体按需加载。

#### 2.2 四种激活模式（参考 Windsurf）

| 模式 | 前缀内容 | 加载时机 | 适用场景 |
|---|---|---|---|
| `always_on` | **全文** | 每次启动 | 关键约束（"永远不要改数据库"） |
| `model_decision` | **索引描述** | `read_file` 按需 | 一般事实（默认模式） |
| `glob` | **索引描述** | 仅当匹配的文件被引用时 | 特定模块约定 |
| `manual` | **仅名称** | 仅通过 `@事实名` 引用 | 大型参考文档 |

**新事实默认 `model_decision`**。旧事实迁移默认也设为此模式。

#### 2.3 缓存管理

```
启动时:
  memory.Load()
    ├── 指令文档 → 编译进系统 prompt（缓存）
    ├── MEMORY.md 索引 → 编译进系统 prompt（缓存）
    └── always_on 事实 → 编译进系统 prompt（缓存）
                                 ↓
                          前缀缓存命中（零 token 成本）

会话中:
  用户: 记住 X
  remember("name", "body")
    ├── 写入磁盘
    ├── 更新 MEMORY.md 索引
    ├── 刷新内存缓存
    └── QueueMemory("Learned: X 是一个好的实践\n  ...")
                                 ↓
  下一轮 Compose(): <memory-update>Learned: X 是一个好的实践</memory-update>
                                 ↓
                   注入到 user message（非系统 prompt）
                                 ↓
                   前缀不变，缓存仍命中
```

#### 2.4 前缀容量保护

| 机制 | 阈值 | 行为 |
|---|---|---|
| 索引行数上限 | 200 行 | 超出的条目从索引中截断（按 recency 排序） |
| always_on 总大小 | 4KB | 超出的拒绝 + 提示用户改用 `model_decision` |
| 索引文件大小 | 8KB | 超过时警告，提示精简描述 |

### 第三层：会话摘要（Session Summaries）

**新机制**。当会话结束时，AI 自动总结关键事实并追加到自动记忆存储。实现方式：

```go
type SessionSummary struct {
    Learned       []string `json:"learned"`       // 本会话学到的新事实
    ChangedRules  []string `json:"changed_rules"`  // 用户要求改变的做法
    FollowUps     []string `json:"follow_ups"`     // 未完成的事项
}
```

- 保存到 session meta 文件中
- 下次启动时，系统读取最近的 N 条摘要，提取新事实
- 工具 `remember` 已经有去重逻辑（`name` slug 相同即覆盖）

### 边缘情况与解决

| 场景 | 方案 |
|---|---|
| **记忆膨胀**（500条+） | 索引截断后 200 条；按 recency 排序；旧条目仍在磁盘，可被 `read_file` 读取 |
| **同 session 内事实更新** | turn-tail 注入，前缀不变；模型通过 `<memory-update>` 感知变化 |
| **跨 session 冲突** | last-writer-wins；同 slug 名覆盖，不同 slug 共存 |
| **用户移除项目** | `forget` 从 index 删除条目；重启后 index 自动重建（从磁盘读取最新） |
| **前缀缓存失效** | 系统 prompt 发生变化 → 前缀缓存丢失 → 需一次冷调用重建。应减少前缀变化频率 |
| **always_on 溢出** | 提示用户降低模式 |
| **主题目录删除** | 扫描时自动检测目录消失，从 index 移除 |
| **@import 循环** | 现有 5 层上限 + 循环检测（已在 `doc.go` 实现） |
| **记忆过期** | 事实可能因项目变更而过时，无主动检测机制 |
| **矛盾记忆** | 两条事实描述同一事物但内容冲突，模型不知该信任哪个 |

### 业界调研：记忆校验现状

调研了 Claude Code、Windsurf/Cascade、Cursor、GitHub Copilot 四个主流工具后，结论是：

**没有任何主流工具做了主动记忆校验。**

| 工具 | 自动记忆 | 主动过期检测 | 矛盾检测 | forget 工具 |
|---|---|---|---|---|
| Claude Code | ✓ | ✗ 仅提示"可能已过时" | ✗ 文档警告"可能任意选择" | ✓ |
| Windsurf | ✓ | ✗ | ✗ | ✗ 只能手动删文件 |
| Cursor | ✗ 纯规则文件 | N/A | N/A | N/A |
| Copilot | ✗ | N/A | N/A | N/A |

所有工具的共同模式：**记忆写入后永久有效，全靠模型在工作中碰巧发现事实不对了，再调用 `forget` 更新。没有后台校验管道。**

### P2.5 记忆校验（新增）

在 P2（激活模式）之后插入此阶段。为记忆系统增加**被动校验**能力，弥补业界空白。

#### 校验元信息

每条事实在存储时附带以下元数据（放在事实文件的 frontmatter 中，不进入索引，不影响前缀缓存）：

```yaml
---
name: db-config
type: project
created_at: 2026-06-01T10:00:00Z
updated_at: 2026-06-09T14:30:00Z
verified_at: 2026-06-09T14:30:00Z
verify_count: 3
refs:
  - config/database.yml
  - .env.example
activation: model_decision
---
```

#### 三种校验策略

| 策略 | 触发时机 | 做法 |
|---|---|---|
| **启动时浅校验** | 每次启动，仅对 `always_on` 事实 | 扫描事实中引用的文件路径（`refs`），检查是否存在。不存在则标记为 `[可能过期]` |
| **引用时校验** | 模型通过 `read_file` 读取事实时 | 返回内容时附带时间戳信息，模型的系统 prompt 中已含"事实可能已过时，使用前请验证"的提示 |
| **定期重校验** | 每 N 次启动 / 配置的间隔 | 读取所有 `model_decision` 事实，对有 `refs` 的做路径存在性检查；启用 `always_on` 超过 30 天未更新的提示用户审阅 |

#### 在索引中的表现

```
MEMORY.md:
- [数据库配置](db-config.md) — PostgreSQL 连接信息       [已验证 2026-06-09]
- [旧版配置](old-config.md) — MySQL 连接信息             [可能过期]
- [临时调试](temp-debug.md) — 当前 bug 的临时绕过方案     [2026-06-01后未更新]
```

#### 边界情况

| 场景 | 方案 |
|---|---|
| **文件路径移动但事实仍正确** | 校验标记 `[可能过期]`，但模型可自行判断是否要 `remember` 更新路径 |
| **always_on 事实 30 天未更新** | 启动时提示用户审阅，不自动删除 |
| **校验发现 refs 路径不存在** | 降级为 `model_decision` 并标记 `[可能过期]`，不移除（用户可能只是暂时删了文件）|
| **与现有 `remember`/`forget` 工具兼容** | 完全兼容。`remember` 更新事实时更新 `updated_at`；`forget` 删除时不校验 |
| **前缀缓存影响** | **零影响**。校验元数据存放在事实文件的 frontmatter 中，不在索引内，不影响系统 prompt |
| **校验开销** | 仅做路径存在性检查（`os.Stat`），无网络调用，每次启动 < 1ms |

#### 为什么这个设计是合理的

1. **不引入向量/外部依赖** — 纯文件系统校验，无冷启动延迟
2. **不破坏前缀缓存** — 校验标记放在文件和索引中，不在系统 prompt 内
3. **不依赖模型主动性** — 被动检测 + 可视化标记，比 Claude Code 纯靠模型发现更可靠
4. **渐进增强** — 新事实自动带校验元数据，旧事实无 `refs` 则跳过校验，无迁移成本

---

## 实现阶段

| 阶段 | 内容 | 涉及文件 | 估时 |
|---|---|---|---|
| **P1** | MEMORY.md 索引重构：改为精简一行描述，不含全文；索引截断 200 行 | `internal/memory/store.go`, `render.go` | 2d |
| **P2** | 引入激活模式 frontmatter + 加载时按模式处理 | `internal/memory/store.go` | 2d |
| **P3** | 解析器支持 topic 目录（子目录对应主题） | `internal/memory/store.go` | 1d |
| **P4** | 系统 prompt 中只包含 index + always_on 事实，model_decision 事实按需加载 | `internal/memory/memory.go`, `internal/boot/boot.go` | 2d |
| **P5** | Doc 编辑后的差异化注入（全文 → diff） | `internal/control/controller.go`, `input.go` | 1d |
| **P6** | 会话摘要（session end hook → 自动提取事实） | `internal/control/controller.go`, `docs/SESSION_SUMMARY.md` | 2d |
| **P7** | 桌面端 UI：索引可折叠显示、激活模式切换、index 预览 | `desktop/frontend/src/components/MemoryPanel.tsx` | 2d |
| **P8** | CLI `/memory` 命令增强：搜索、模式切换、按主题列出 | `internal/control/slash.go`, `internal/cli/` | 1d |
| **P9** | 前缀容量保护：索引行数上限、always_on 大小限制 | `internal/memory/memory.go` | 1d |
| **P10** | 测试 & 迁移：旧事实自动迁移为 `model_decision` 模式 | `internal/memory/` | 1d |
| **合计** | | | **~15d** |

---

## 与现有系统的兼容性

| 现有功能 | 兼容性 | 说明 |
|---|---|---|
| `remember` 工具 | ✓ 完全兼容 | 新增事实默认 `model_decision` |
| `forget` 工具 | ✓ 完全兼容 | 从 index 删除 + 删除文件 |
| `# 快速记录` | ✓ 完全兼容 | 写入 doc 文件 `## Notes` 章节 |
| `/memory` 命令 | ✓ 增强 | 新增搜索、模式切换 |
| 桌面端 MemoryPanel | ✓ 增强 | 新增可折叠索引、模式切换 |
| `@-reference` | ✓ 不受影响 | 与记忆系统独立 |
| 前缀缓存 | ✓ 改进 | 事实不再全文载入，前缀更紧凑 |

---

## 为什么这个方案是"完美"的

1. **缓存零损失**：index-only 前缀比当前全量编译更紧凑，且始终稳定
2. **按需加载**：模型只读取它觉得相关的事实，不浪费 token
3. **激活模式**：让用户控制记忆的上下文预算——关键规则 always_on，一般事实 model_decision
4. **自动收敛**：索引行数上限确保记忆不会无限膨胀
5. **会话摘要**：端到端的自动记忆闭环——每次会话的 learnings 自动持久化
6. **DeepSeek 原生适配**：整个方案围绕自动前缀缓存设计，最大限度利用 DeepSeek 的缓存能力
7. **无向量依赖**：纯文本系统，无外部依赖，无冷启动延迟
8. **渐进迁移**：旧事实自动迁移，用户无感知
