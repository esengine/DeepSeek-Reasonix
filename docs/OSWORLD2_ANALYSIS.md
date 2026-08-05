# OSWorld 2.0 对标分析：Reasonix 长程任务调整

> 基于 XLANG Lab 发布的 OSWorld 2.0 基准测试（2026.6），分析 Reasonix 在长程任务上的差距并实施针对性调整。

## 1. OSWorld 2.0 核心发现

### 1.1 基准规模

| 指标 | OSWorld 1.0 | OSWorld 2.0 | 倍数 |
|------|------------|-------------|------|
| 任务数 | 369 | 108 | — |
| 人类完成中位时间 | ~2 分钟 | ~1.6 小时 | 48× |
| Agent 平均工具调用 | ~30 | ~318 | 10.6× |
| 跨应用数 | 1-2 | 平均 2.44 | — |
| 评分检查点 | — | 平均 27.25/任务 | — |

### 1.2 模型表现

| 模型 | 推理强度 | 调用方式 | 完成率 | 部分分 |
|------|---------|---------|--------|--------|
| Claude Opus 4.8 | max | 批量工具 | **20.6%** | 54.8% |
| Claude Opus 4.8 | max | 标准 | 18.5% | 49.3% |
| Claude Opus 4.7 | max | 批量工具 | 18.2% | 48.9% |
| GPT-5.5 | xhigh | 批量工具 | 13% | 49.5% |
| Claude Sonnet 4.6 | max | 标准 | 8.3% | 41.5% |
| MiniMax M3 | enabled | 标准 | 4.6% | 22.3% |
| Qwen 3.7 Plus | thinking | 标准 | 2.8% | 21.5% |

**关键结论**：即使是最好的模型也只完成了 20.6% 的任务，远未达到专业级计算机使用水平。

### 1.3 十类挑战现象

OSWorld 2.0 识别了真实工作流中 10 类反复出现、却被旧基准忽略的难点：

| # | 挑战 | 描述 | 失败模式 |
|---|------|------|---------|
| 1 | **流式交互** | 环境在"看一眼"和"动手"之间持续变化 | 点击坐标永远差一点 |
| 2 | **动态环境** | 执行途中需求被新信息改写 | 死守初始计划，最终表单错误 |
| 3 | **教程跟随** | 边看 PDF/视频教程边操作 | 视频抽帧丢失时序，复现不出操作 |
| 4 | **主动交互** | 信息不全时该停下来问用户 | 硬着头皮猜，而不是求证 |
| 5 | **跨源推理** | 银行流水、邮件回执、政策文件对账 | 从单一页面照抄，不做交叉验证 |
| 6 | **隐状态推断** | 信息没有直接来源，需要推断"藏在哪里" | 不去找隐藏信息，直接跳过 |
| 7 | **视觉空间精度** | 像素级定位、几何与对齐 | 点击位置偏差 |
| 8 | **多模态编辑** | 图像、视频、音频的编辑操作 | 无法正确操作非文本内容 |
| 9 | **冲突消歧** | 多个来源信息矛盾 | 不识别矛盾，或识别后不问用户 |
| 10 | **验证缺失** | 跳过最终验证步骤 | 声称完成但实际未完成 |

### 1.4 核心瓶颈

> "Agents lose track of constraints, miss information that arrives mid-task, guess rather than ask the user, and skip verification, struggling most when a task hinges on hidden state they must recover."

**性能崩溃点**：任务超过 ~2.5 小时后，agent 性能急剧下降——隐式状态丢失是首要原因。

---

## 2. Reasonix 现状分析

### 2.1 已有的长程任务支持

| 机制 | 位置 | 状态 | 说明 |
|------|------|------|------|
| **无界步数** | `config.go:MaxSteps=0` | ✅ 良好 | 交互模式默认无步数上限，依赖自适应保护 |
| **三阶段压缩** | `compact.go:maybeCompact` | ✅ 良好 | soft(0.5) → snip(0.6) → compact(0.8) 三档触发 |
| **7 节摘要** | `compact.go:summarySystemPrompt` | ✅ 良好 | 约束/目标/决策/文件/命令/错误/待办 |
| **摘要累积** | `compact.go:partitionFold` | ✅ 良好 | 之前摘要原样保留，不二次摘要 |
| **用户消息钉住** | `compact.go:pinnableUserTurn` | ✅ 良好 | ≤1500 token 的用户消息永不摘要 |
| **Storm Breaker** | `agent.go:applyStormBreaker` | ✅ 良好 | 同一工具连续失败 3 次自动中断 |
| **Todo 停滞检测** | `agent.go:todoStallPause` | ✅ 良好 | 8 轮 nudge，16 轮暂停 |
| **Steer 机制** | `agent.go:Steer/flushSteerQueue` | ✅ 良好 | mid-turn 用户指导注入，LocalOnly 跨压缩 |
| **工具结果剪枝** | `prune.go:SnipStaleToolResults` | ✅ 良好 | snip(首尾保留) → prune(占位符) → archive |
| **`todoState`** | `agent.go:todoState` | ✅ 良好 | 跨 turn 和压缩保留，不进入 prompt |
| **run_mcp 元工具** | `plugin/metatool.go` | ✅ 刚完成 | 工具数组从 114 降至 20，减少注意力稀释 |

### 2.2 差距识别

| OSWorld 2.0 挑战 | Reasonix 缺口 | 影响 |
|------------------|-------------|------|
| **隐状态丢失 (#6)** | 压缩摘要无"隐式状态"专节，推断/恢复的信息可能被折叠 | **高**：OSWorld 2.0 #1 失败模式 |
| **跨源推理 (#5)** | 无"已查源"追踪，压缩后可能重查同一源 | **中**：浪费工具调用预算 |
| **猜测而非询问 (#4)** | 无"未解决问题"显式追踪，agent 倾向猜测而非 ask | **中**：导致错误传播 |
| **验证缺失 (#10)** | 无周期性验证提醒，长程任务中 agent 容易跳过验证 | **中**：完成率下降 |
| **长程压缩调优** | 压缩阈值(0.5/0.6/0.8)对 318+ 步任务可能过晚触发 | **中**：隐式状态在 snip 阶段丢失 |
| **流式交互 (#1)** | 需要环境层支持，超出 agent 核心范围 | **低**（本次不处理） |
| **动态环境 (#2)** | Steer 机制存在但依赖用户主动注入 | **低**（本次不处理） |

---

## 3. 已实施的调整

### 3.1 增强压缩摘要：3 个新节

**文件**：`internal/agent/compact.go` — `summarySystemPrompt`

在原有的 7 节摘要基础上，新增 3 个直接对标 OSWorld 2.0 失败模式的节：

| 新节 | 对标挑战 | 作用 |
|------|---------|------|
| `## Hidden state & recovered facts` | 隐状态推断 (#6) | 保留从间接来源推断/恢复的信息——隐藏路径、隐式配置、从日志/错误中重建的状态。这是长程任务中最容易丢失的部分 |
| `## Sources consulted` | 跨源推理 (#5) | 追踪已查和未查的数据源——避免压缩后重查同一源，标记未探索的可能信息源 |
| `## Open questions & uncertainties` | 主动交互 (#4) | 显式列出未解决问题和需要用户确认的假设——nudge agent 在不确定时 ask 而非 guess |

**效果**：压缩后 agent 看到的摘要从 7 节变为 10 节，新增的 3 节直接捕获 OSWorld 2.0 识别的前 3 大失败模式。摘要累积机制保证这些信息一旦进入摘要就永远不会被"二次摘要"丢失。

### 3.2 长程模式配置项

**文件**：`internal/config/config.go` + `internal/config/load.go`

新增 `[agent]` 配置项：

```toml
[agent]
long_horizon = true              # 启用长程模式
verification_interval = 50       # 验证 nudge 间隔（步数），默认 50
```

**`LongHorizonEnabled()` 方法**支持三层优先级（与 `meta_tool` 相同的模式）：

1. `REASONIX_LONG_HORIZON` env 变量（覆盖层）
2. `[agent] long_horizon` 配置项（持久化）
3. 默认 `false`（保持现有行为）

**`normalizeLongHorizon()` 归一化函数**在启用时调整：

| 参数 | 标准默认 | 长程模式 | 效果 |
|------|---------|---------|------|
| `SoftCompactRatio` | 0.5 | **0.4** | 更早发出上下文增长通知 |
| `ToolResultSnipRatio` | 0.6 | **0.5** | 更早开始剪枝旧工具结果 |
| `CompactRatio` | 0.8 | 0.8（不变） | 压缩触发点不变 |
| `VerificationInterval` | 0（禁用） | **50** | 每 50 步验证 nudge |

**设计原则**：
- 仅调整标准默认值（0.5/0.6），用户显式设置的其他值不受影响
- 压缩触发点（0.8）不变——不改变"何时压缩"，只改变"何时开始准备压缩"
- 更早的 soft/snip 给 agent 更多 runway 在硬折叠前捕获隐式状态

### 3.3 验证间隔配置

`VerificationInterval` 字段为未来的验证 nudge 机制预留接口。当前仅完成配置基础设施（字段 + 归一化 + env 覆盖），验证 nudge 的运行时注入逻辑将在下一阶段实现。

---

## 4. 使用方式

### 4.1 启用长程模式

在项目的 `reasonix.toml` 中：

```toml
[agent]
long_horizon = true
```

或通过环境变量临时启用：

```bash
# 启用
REASONIX_LONG_HORIZON=1 reasonix

# 禁用（覆盖配置中的 true）
REASONIX_LONG_HORIZON=0 reasonix
```

### 4.2 自定义压缩阈值

长程模式下仍可显式覆盖阈值：

```toml
[agent]
long_horizon = true
soft_compact_ratio = 0.35    # 比长程默认 0.4 更早
tool_result_snip_ratio = 0.45 # 比长程默认 0.5 更早
verification_interval = 30    # 比默认 50 更频繁
```

显式设置的值不会被 `normalizeLongHorizon` 覆盖。

### 4.3 与 run_mcp 元工具组合

长程模式 + 元工具是最优组合：

```toml
[tools]
meta_tool = true       # 工具数组 114 → 20

[agent]
long_horizon = true    # 更早压缩 + 隐式状态保留
```

**协同效应**：
- `run_mcp` 减少每步注意力消耗（20 vs 114 条工具 schema）
- `long_horizon` 确保隐式状态在压缩时被捕获
- 两者共同作用于 OSWorld 2.0 的两大瓶颈：注意力稀释 + 隐式状态丢失

---

## 5. OSWorld 2.0 挑战 vs Reasonix 调整对照

| OSWorld 2.0 挑战 | Reasonix 调整 | 覆盖状态 |
|------------------|-------------|---------|
| 隐状态推断 (#6) | 摘要新增 `Hidden state & recovered facts` 节 + 更早压缩 | ✅ 已覆盖 |
| 跨源推理 (#5) | 摘要新增 `Sources consulted` 节 | ✅ 已覆盖 |
| 主动交互 (#4) | 摘要新增 `Open questions & uncertainties` 节 | ✅ 已覆盖 |
| 验证缺失 (#10) | `VerificationInterval` 配置基础设施 | ⏳ 部分覆盖（运行时注入待实现） |
| 注意力稀释 | `run_mcp` 元工具（114→20 工具） | ✅ 已覆盖（独立 PR） |
| 长程压缩调优 | `long_horizon` 模式（更早 soft/snip） | ✅ 已覆盖 |
| 流式交互 (#1) | 需环境层支持 | ❌ 未覆盖（超出 agent 范围） |
| 动态环境 (#2) | Steer 机制已存在 | ✅ 已有（依赖用户主动注入） |
| 教程跟随 (#3) | 需多模态能力 | ❌ 未覆盖（超出当前范围） |
| 视觉空间精度 (#7) | 需视觉模型能力 | ❌ 未覆盖（模型层问题） |
| 多模态编辑 (#8) | 需多模态工具支持 | ❌ 未覆盖 |
| 冲突消歧 (#9) | `Open questions` 节部分覆盖 | ⏳ 部分覆盖 |

---

## 6. 文件变更清单

| 文件 | 变更 | 说明 |
|------|------|------|
| `internal/agent/compact.go` | 修改 `summarySystemPrompt` | 新增 3 个摘要节 |
| `internal/config/config.go` | 新增 `LongHorizon` + `VerificationInterval` 字段；新增 `LongHorizonEnabled()` + `longHorizonEnvOverride()` | 配置项 + env 覆盖 |
| `internal/config/load.go` | 新增 `normalizeLongHorizon()` 函数；在 `loadForRoot` 中调用 | 归一化逻辑 |

---

## 7. 后续工作

### 7.1 验证 Nudge 运行时注入（下一步）

`VerificationInterval` 配置已就位，但运行时注入逻辑未实现。计划在 `run_loop.go` 的 `runToolLoop` 中添加：

```go
// 伪代码
if step > 0 && step % verificationInterval == 0 {
    injectVerificationNudge() // 提醒 agent 检查进度、验证结果、列出未解决问题
}
```

### 7.2 批量工具调用

OSWorld 2.0 数据显示 Claude Opus 4.8 使用"批量工具"模式比"标准"模式高 2.1 个百分点（20.6% vs 18.5%）。Reasonix 当前不支持批量工具调用——模型每轮只能调用一个工具。实现批量工具调用可以减少 round-trip 开销。

### 7.3 隐式状态追踪器

当前隐式状态依赖压缩摘要的文本描述。可以考虑在 agent 层维护一个结构化的 `implicitState` map（类似 `todoState`），不进入 prompt 但在压缩时作为摘要输入的补充。

### 7.4 跨源追踪器

类似 `implicitState`，维护一个 `sourcesConsulted` set，自动从工具调用中提取源信息（文件路径、URL、API 端点），在压缩时注入摘要输入。

---

## 8. 参考资料

- [OSWorld 2.0 论文](https://arxiv.org/pdf/2606.29537v1.pdf)
- [OSWorld 2.0 官网](https://osworld-v2.xlang.ai)
- [OSWorld-V2 代码](https://github.com/xlang-ai/OSWorld-V2)
- [Snorkel OSWorld 2.0 排行榜](https://snorkel.ai/leaderboard/os-world-2-0/)
- [腾讯云分析文章](https://cloud.tencent.com.cn/developer/article/2701451)
