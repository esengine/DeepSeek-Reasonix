# Cache-Aware Context Projection 与惰性压缩

> 日期：2026-08-07
> 状态：当前实现说明
> 核心约束：canonical transcript 是永久事实源；缓存状态只影响成本策略，不直接触发历史改写。

## 一、问题与目标

长会话需要同时满足两个目标：

1. 保留完整历史，以支持恢复、回退、分支和审计；
2. 在模型上下文接近上限时，构造更短且稳定的 provider-visible 请求。

旧路径把 cache TTL 过期与 session 压缩绑定：cold resume 会同步压缩并重写历史。这既把成本信号变成了数据变更信号，也会在用户只是恢复会话时破坏原有前缀。

当前设计将两者拆开：

```text
canonical transcript (Session.Messages，普通压缩永不改写)
    |
    +-- model-visible context projection
    |
    +-- cache state (warm/cold/unknown，仅参与成本与观测)
```

## 二、持久化边界

### Canonical transcript

- `Session.Messages` 始终保存完整 transcript。
- 普通 compaction、cold resume、tool prune/snip 不删除或替换 canonical 消息。
- rewind、fork、branch 仍以 canonical 为事实源。

### Context projection sidecar

- projection 存储在 `<session>.context.json`，不改变原 session 文件格式。
- sidecar 保存 projection、covered prefix fingerprint、transcript/projection version、prompt cache key、cache 状态和压缩 telemetry。
- 删除 session 时 sidecar 纳入同一删除清单。
- 老版本不知道 sidecar 时仍可读取完整 session；新版本遇到缺失、旧 schema 或校验失败的 sidecar 会安全重建。

## 三、运行时行为

### Resume 只记录缓存状态

恢复会话时，根据 provider TTL 和最后活动时间记录 `warm`、`cold` 或 `unknown`。Resume 路径不会调用 `Compact`、`SnapshotRewrite` 或 `PruneStaleToolResults`，也不会修改 canonical transcript。

### Preflight 惰性生成 projection

每次模型请求前，`contextPreflight` 根据当前 token 压力判断是否需要 projection：

- 未达到压力阈值：继续发送 append-only canonical view；
- 达到压缩阈值：尝试生成并安装 projection；
- 达到 force 阈值但没有可折叠内容：在非 tool loop 中返回可重试的 `ErrCompactionRequired`；
- 摘要失败：不写 mechanical marker，不安装半成品，也不改写 canonical；
- tool loop 进行中：只发 notice，由后续 preflight/stuck guard 处理，避免中断工具调用配对。

### Provider-visible 顺序

projection 使用稳定顺序：

```text
system
-> 确定性的早期 user turns
-> 一条 rolling summary
-> 必须保留的消息
-> recent tail
```

早期 user turn 的资格使用固定 token/char 估算和 context-window 上限，不依赖最近一次 provider usage。动态 usage 校准只用于 tail sizing 等不决定消息身份的估算，因此 projection 激活后，早期前缀不会因 canonical/projection 统计口径变化而漂移。

旧 summary 会进入下一次 fold，由新 summary 滚动吸收；provider-visible projection 始终只保留一条 summary，不会形成无限摘要链。原 summary 仍保留在 canonical transcript 中。

## 四、有效性与失效

projection 采用 fail-closed 校验：

- `CoveredPrefixHash` 对 `ModelMessages(canonical[:CoveredCount])` 的完整 provider-visible 内容生成稳定 fingerprint，覆盖图片、reasoning 元数据、Responses items、tool call ID/name/arguments/thought signature；
- `PromptCacheKey` 必须存在，并严格匹配 `workspace|session lineage|model`；
- 缺少 fingerprint、前缀被 edit/rewrite、切换模型或 lineage 时，内存 projection 立即失效；
- rewind、fork、branch、snip/prune 和显式范围摘要会使相关 projection 失效或隔离。

加载时发现某模型的 sidecar key 不匹配，只丢弃当前内存状态，不删除磁盘文件，避免破坏其它模型仍可使用的状态。

## 五、Provider compaction 与失败策略

Provider 接口已定义：

- `NativeCompactor`；
- `CompactionRequest` / `CompactionResult`；
- `CompactionCapabilities`；
- `ErrCompactionUnsupported`。

当前 Responses vendor 明确返回 unsupported，并回退到 Reasonix 的摘要路径。摘要首次失败后的重试会聚合两次 attempt 的 usage 与 request count，供成本和 telemetry 使用。

Anthropic、DeepSeek 等原生 compaction endpoint 尚未接入；能力接口不代表这些 endpoint 已经可用。

## 六、缓存影响

- 正向影响：cold resume 不再为了 TTL 状态改写历史，缓存仍 warm 时可以继续复用原 append-only 前缀。
- 预期 miss：首次在高压力下激活 projection 时，请求前缀会从 canonical 切换为 `summary + tail`，因此会发生一次预期的 cache miss。
- 稳定性：激活后，确定性的早期轮次、单条 rolling summary、稳定 cache key 和 fail-closed fingerprint 降低后续无意义的前缀漂移。
- 不确定点：token 估算可能使 preflight 比旧路径更早或更晚触发摘要，需要通过 telemetry 持续观察 break-even 成本。

## 七、明确未实现的后续

以下能力不属于当前阶段，不能按已落地行为依赖：

1. Anthropic/DeepSeek 原生 compaction endpoint；
2. compaction 后调用 `SaveKnowledge` 或写入 EventChain；
3. 依靠 EventChain 完成跨 session 的 L2 自动恢复；
4. feature flag 观测期、旧兼容路径的最终清理；
5. 完整 break-even 成本 dashboard 聚合。

这些后续必须分别设计失败原子性、持久化兼容、缓存影响和 provider 能力探测，不能重新把 cache TTL 与 canonical transcript 改写绑定。
