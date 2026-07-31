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
