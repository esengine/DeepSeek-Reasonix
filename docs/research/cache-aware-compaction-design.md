# Content-Driven Context Maintenance（Cache-Aware Checkpoint）

> 日期：2026-08-10
> 状态：当前实现说明（取代多阈值 prune/snip/native 自动维护叙述）
> 核心约束：canonical transcript 是永久事实源；唯一自动触发是 `compact_ratio`；缓存状态只影响成本与观测，不触发历史改写。

## 一、问题与目标

长会话需要同时满足：

1. 保留完整历史，以支持恢复、回退、分支和审计；
2. 在上下文接近上限时，构造更短且稳定的 provider-visible 请求；
3. 不因 cache TTL / cold resume 主动改写仍可命中的前缀。

旧路径使用 soft / snip / force 多阈值，并在压力下自动安装 prune 投影或调用 provider native compaction。该路径把维护成本与可恢复性缠在一起，也会在 resume 时破坏缓存前缀。

当前产品路径：

```text
canonical transcript (Session.Messages，普通维护永不改写)
    |
    +-- model-visible context projection / checkpoint
    |       system + one structured summary + recent 16% tail
    |
    +-- compatibility tool storage (32KB Content + full RawContent)
    |
    +-- cache state (warm/cold/unknown，仅成本与观测)
```

## 二、唯一自动触发

- 配置键：`agent.compact_ratio`（默认 `0.80`）
- 入口：`Prepare` / preflight 是唯一自动维护入口；`ObserveUsage` 只更新统计
- 不再存在自动 soft compact 或 native multi-threshold 路径；达到压力后先提交 tool-result prune 投影
- 兼容：旧配置键与 v3 sidecar 字段可读；prune 不提升 schema

## 三、Checkpoint 形态

当 projected tokens ≥ `compact_ratio × context_window` 时，生成内容驱动 checkpoint：

```text
stable system / early prefix
-> 一条结构化 summary（单次摘要请求，上限 8192）
-> recent tail（固定约 16% 窗口）
```

验收要点：

- 候选必须严格小于被替换的完整请求，并通过同一 estimator/准入路径
- 摘要失败不写 mechanical marker，不安装半成品，不改 canonical
- provider-visible 始终最多一条 summary；旧 summary 可进入下一次 fold 被滚动吸收
- 首次安装会预期 cache miss；安装后前缀应保持稳定以利后续 hit

## 四、持久化边界

### Canonical transcript

- `Session.Messages` 始终保存完整 transcript
- 普通 compaction、cold resume、旧 prune/snip API no-op 均不删除或替换 canonical 消息
- rewind / fork / branch 仍以 canonical 为事实源

### Context projection sidecar

- 路径：`<session>.context.json`（schema v3）
- 保存 projection、covered prefix fingerprint、version、prompt cache key、cache 状态与 telemetry
- 旧 prune / native 字段可加载后忽略；校验失败则安全重建
- 删除 session 时 sidecar 一并删除

## 五、运行时行为

### Resume

只根据 provider TTL 与最后活动时间记录 `warm` / `cold` / `unknown`。Resume 不调用 Compact、不安装 projection、不改写 tool results。

### Prepare

每次模型请求前：

1. 估计 projected tokens
2. 低于 `compact_ratio`：发送 append-only / 现有有效 projection
3. 达到阈值：先持久 prune；不足时至多两次 summary，逐次 CAS 安装 checkpoint
4. overflow：至多一次 prune、一次 summary、一次原请求重试

### Tool-result compatibility storage

工具结果创建时把兼容字段 `Content` 限制在约 32KB，完整原文进 `RawContent`。新版本在低压普通请求中临时提升 `RawContent`，因此模型看到全文；只有达到压力阈值或 overflow 才安装 4096/marker/1024 的持久 prune projection。manual `/compact` 不自动 prune。

## 六、Provider 与输出预算

- 应用层 summary 是默认路径；Responses 等 native compaction 标记 unsupported 时回退 summary
- `max_output_tokens=0` 在官方 DeepSeek 上省略该字段（服务端 384K 上限）；MiMo 等仍用 16K/32K 梯子。思考深度只走 effort。
- auto ladder 与 `compact_ratio` 解耦

## 七、缓存影响

| 场景 | 预期 |
| --- | --- |
| warm resume 低于阈值 | 复用 append-only 前缀，无摘要 |
| 首次跨过 compact_ratio | 先 prune；必要时前缀变为 system+summary+tail，一次预期 miss |
| checkpoint 安装后继续对话 | 稳定 prefix 利于 hit；generation 作用域避免重复摘要 |
| cold resume | 只记 cache 状态，不因 TTL 重写历史 |

## 八、验证与烟雾

- 确定性：`internal/agent` compact / projection / pressure-prune / restart 测试
- 离线 e2e：`benchmarks/context-maintenance-e2e` 的 `seed` + `resume`（`-offline`）
- 在线 e2e：同目录 `continue`（`DEEPSEEK_API_KEY`，`-max-usd` 费用上限，至多一次摘要）

## 九、有意保留的兼容层

不算功能缺口，也不声称“代码里已无旧概念”：

1. 配置结构体仍可读旧 soft/snip/force 键，加载时清零并迁移删除
2. sidecar 仍可解码旧 prune/native 字段后忽略
3. `PruneStaleToolResults` / `SnipStaleToolResults` 保留为 no-op API，避免旧调用点 panic
4. `Content + RawContent` 双字段继续保证新旧版本都能安全读取同一 session

## 十、明确未做

1. 重新启用多阈值自动 prune/snip 投影
2. 把 cache TTL 重新绑定到 transcript 改写
3. 跨 session 的 EventChain L2 自动恢复作为维护主路径
4. 完整 break-even 成本 dashboard
