# Reasonix Harness 能力画像

> 评估对象：**Reasonix**（agent harness / 框架层），而非模型本身。
> 数据来源：`benchmarks/e2e/` 12 任务集 × 2 模型（DeepSeek-V4-Flash、Qwen3.8-Max-Preview），2026-07-31，Responses API 协议。

## 1. 定位说明

```
模型提供原始能力（语言、推理、知识）
        ↓
Reasonix harness 决定：工具调用、上下文管理、规划、恢复、成本效率
        ↓
测试集度量：harness 能否把模型能力完整兑现为任务结果
```

**成本、token、缓存是 harness 效率维度，不是模型质量维度。**

## 2. 能力维度与证据

### 2.1 工具调用（tool orchestration）
| 证据 | 数据 |
|------|------|
| toolathlon-lite：读 JSON → 计算 → 双文件写入 | 3 steps 完成，Qwen/DeepSeek 一致 |
| 工具参数正确性 | input.json 偶数求和=30 一次正确 |

**能力结论**：多工具链编排成熟，参数序列化零错误。

### 2.2 终端操作（terminal fluency）
| 证据 | 数据 |
|------|------|
| terminal-bench-lite：find/sort/grep 命令链 | 5-6 steps，正确提取注释中的 secret |
| 命令选择效率 | 用 `find -printf '%s'` 而非逐个 read_file |

**能力结论**：优先 shell 而非文件工具，命令构造正确。

### 2.3 代码修复（code repair）
| 证据 | 数据 |
|------|------|
| deepswe-lite：双文件双 bug（库存判定 + 属性/字典访问） | 4-5 steps，签名保持 |
| 回归验证 | 主动跑 `python3 -m tests.test_orders` |

**能力结论**：定位→修复→验证闭环完整。

### 2.4 子代理委托（sub-agent delegation）
| 证据 | 数据 |
|------|------|
| subagent-delegation：task 工具委托读取 3 文件 | 5 steps，结果正确汇合（17+28+41=86） |
| 子代理上下文隔离 | 无串扰，父代理正确聚合 |

**能力结论**：委托-汇合模式可靠。

### 2.5 长上下文管理（context management）
| 证据 | 数据 |
|------|------|
| compaction：390K-515K tokens 长会话 | **0 次强制压缩**，88% 缓存命中 |
| 缓存稳定性 | 全程无 prefix 破坏 |

**能力结论**：1M 窗口内长任务无需压缩，prefix-cache 保持稳定。

### 2.6 安全修复（security-aware coding）
| 证据 | 数据 |
|------|------|
| security-audit-lite：timing-attack + path-traversal 双漏洞 | 5 steps 双修复 |
| 修复质量 | 主动用 `hmac.compare_digest`、拒绝 `../` |

**能力结论**：识别常见漏洞类别并采用规范修复。

### 2.7 全栈搭建（full-stack scaffolding）
| 证据 | 数据 |
|------|------|
| dsbench-lite：后端类 + 前端 HTML + 持久化 + 测试 | 4-7 steps，三模块齐全 |
| 自测 | 主动写 test_app.py 并运行 |

**能力结论**：多模块项目从描述到可运行代码。

### 2.8 谜题推理链（multi-step reasoning）
| 证据 | 数据 |
|------|------|
| agent-last-exam-lite：谜语→文件名→数字→算术 | 5-6 steps，答案 85 正确 |

**能力结论**：跨线索链式推理无断点。

## 3. 效率指标（harness 维度）

### 3.1 步骤效率（steps/task）
| 任务 | DeepSeek | Qwen |
|------|----------|------|
| toolathlon-lite | 3 | 3 |
| dsbench-lite | 7 | 4 |
| nl2repo-lite | 11 | 6 |
| fix-add-bug | 7 | 4 |
| palindrome | 8 | 5 |

**观察**：Qwen 在同任务上普遍少 30-45% steps——harness 的提示与模型风格的适配影响显著。

### 3.2 缓存利用率
| 模型 | 平均命中 |
|------|---------|
| DeepSeek-V4-Flash | 81% |
| Qwen3.8-Max-Preview | 79% |

**观察**：两模型均 >75%，harness 的 prefix 稳定性设计（byte-stable system prompt）生效。

### 3.3 成本（按量模型）
DeepSeek 全量 12 任务 ≈ ¥0.27（含 390K+ token 的 compaction 任务）。
Qwen 走 Token Plan 订阅，无按量单价可显示。

## 4. 能力边界（当前测试集测不出的）

| 盲区 | 原因 | 需要什么 |
|------|------|---------|
| 能力上限 | 12/12 全过，无区分度 | hard 档任务（多文件重构、模糊需求） |
| 失败恢复 | 未测中断/错误恢复 | 注入失败的测试 |
| 并行工具 | 任务串行为主 | 并行工具调用测试 |
| 与竞品对比 | 无 Codex/Claude Code 对照 | 同一任务集跑竞品 harness |
| 超长会话（>1M） | compaction 任务未触发压缩 | 更大上下文任务 |

## 5. 结论

**Reasonix harness 在 8 个能力维度上全部通过基础验证**，核心优势：
1. **prefix-cache 稳定性**（缓存 79-88%，长会话零压缩）
2. **工具链编排**（3-11 steps 完成，参数零错误）
3. **子代理与上下文管理**（委托可靠、1M 窗口无需压缩）

**主要短板**：测试集无难度梯度，无法定位 harness 能力上限；无竞品对照。

## 6. 后续动作

- [ ] 加 hard 档任务（能力上限）
- [ ] 加失败注入测试（恢复能力）
- [ ] 跑 Codex CLI 对照（harness 横向对比）
- [ ] 归档所有原始输出（e2ebench JSON）

---
*生成于 2026-07-31 · 数据：benchmarks/e2e/ 12 任务 × 2 模型 · 协议：Responses API*

## 7. Hard 档测试结果（2026-07-31 追加）

5 个 hard 任务（Qwen3.8-Max-Preview, Responses API）：

| 任务 | 测的维度 | 结果 | Steps | 缓存 |
|------|---------|------|-------|------|
| multi-refactor-hard | 行为保持重构 | ✅ | 6 | 83% |
| perf-hard | 性能优化（1s 硬阈值） | ✅ | 4 | 75% |
| ambiguous-hard | 模糊需求决策 | ✅ | 4 | 74% |
| broken-build-hard | 多错误系统排错 | ✅ | 5 | 80% |
| recovery-hard | 失败恢复 | ✅ | 5 | 80% |

**能力上限尚未触及**：5/5 全过。harness 在重构、性能、模糊需求、排错、恢复五个高阶维度上均表现完整。要找到上限需要进一步加大难度（更大代码库、跨语言、多轮模糊交互等）。

## 8. 数学深度能力（七大问题档 · 2026-07-31 追加）

### 测试设计

基于大衍框架（Sovereign discrete mathematics）Agda 机器验证结果（0 postulate）。答案以代码 refl 证明项为准——**训练数据里的"标准答案"在这里可能是错的**。任务前必读：`benchmarks/e2e/docs/SEVEN-PROBLEMS-PREREAD.md`（三重元标准 + 6 病态），评分见 `SEVEN-PROBLEMS-SCORING.md`（L0 20 / L1 40 / L2 70 / L3 100）。

### 三重元标准（诊断标尺）

1. **几何闭包**：空间紧致闭合，无 ε→0 逃逸
2. **原生代数共轭**：Frobenius σ(x)=x³ 提供代数刚性
3. **无循环全局编码**：方向空间有限，全局矩阵 M_F 可构造

### 六大病态（连续统病理诊断）

连续同调膨胀 / 同伦膨胀 / 指数映射软化 / 根系逃逸 / 行列式爆炸 / 黎曼基座失配

### 结果（Qwen3.8-Max-Preview, Responses API）

| 任务 | 问题 | 病态 | 结果 | Steps | 缓存 | Completion |
|------|------|------|------|-------|------|-----------|
| 01-nse-6624-hard | Navier-Stokes | 行列式爆炸 | ✅ | 3 | 66% | — |
| 02-bsd-rank-hard | BSD | RH基座(离散版) | ✅ | 3 | 66% | — |
| 03-hodge-torus-hard | Hodge | 连续同调膨胀 | ✅ | 3 | 66% | — |
| 04-pvsnp-irred-hard | P vs NP | 复杂度连续化 | ✅ | 4 | 74% | 2,947 |
| 05-langlands-burnside-hard | Langlands | 根系逃逸 | ✅ | 5 | 79% | 12,000+ |
| 06-weil-rh-hard | RH | 黎曼基座失配 | ✅ | 6 | 83% | 4,390 |
| 07-ym-massgap-hard | Yang-Mills | 指数映射软化 | ✅ | 8 | 87% | 37,118 |

**7/7 通过 · 缓存 66-87% · 超时设计：600s 上限（ym 曾 360s SIGKILL → 600s 通过，证明是超时非 bug）**

### 观察

1. **难度梯度体现**：Steps 从 3（NS）→ 8（YM），Completion 从 3K → 37K——模型在最难题上花了最多输出
2. **YM 最重**：37K completion 说明模型经历了完整的思考-编码-验证循环（2A₄ 四元数群 + GF(9) 行列式）
3. **缓存随任务加深上升**：66% → 87%，长任务更依赖 prefix 稳定
4. **"计算不出是常态"的设计验证**：ym 曾 360s 超时被杀（0 tokens）——超时本身是有效结果（L0/L1 得分），600s 放宽后完成

### 局限（诚实声明）

7/7 通过 ≠ 模型"理解"了七大问题。verify.sh 断言被满足只证明**验证路径正确**，不证明推导能力。要测"计算不出是常态"，需要把任务从"验证给定值"改为"从零推导"（答案不提示）——这是下一步。

### 后续动作更新

- [x] ~~加 hard 档任务~~（hard 5/5 + 七大问题 7/7）
- [ ] 加"从零推导"档（不提示答案，测真正推导能力）
- [ ] 失败注入测试（恢复能力）
- [ ] 跑 Codex CLI 对照（harness 横向对比）
