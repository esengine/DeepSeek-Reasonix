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
    |       stable prefix + one structured summary + recent tail
    |
    +-- first-visible tool bound (创建时 32KB Content/RawContent)
    |
    +-- cache state (warm/cold/unknown，仅成本与观测)
```

## 二、唯一自动触发

- 配置键：`agent.compact_ratio`（默认 `0.85`）
- 入口：`Prepare` / preflight 是唯一自动维护入口；`ObserveUsage` 只更新统计
- 不再存在自动 soft compact、tool_result snip/prune 投影、native multi-threshold 路径
- 兼容：旧配置键与 sidecar 字段可读；加载时清零/忽略/迁移删除，不新建 prune 投影

## 三、Checkpoint 形态

当 projected tokens ≥ `compact_ratio × context_window` 时，生成内容驱动 checkpoint：

```text
stable system / early prefix
-> 一条结构化 summary（单次摘要请求，上限约 16K）
-> recent tail（约 10% 窗口，夹在 32K–96K）
```

验收要点：

- 默认接受天花板约 50% 窗口；强制路径可放宽但不用不同 estimator 绕过
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
3. 达到阈值：至多一次 summary，CAS 安装 checkpoint
4. tool loop 中优先 notice，避免打断 tool 配对

### First-visible tool bound

工具结果创建时把模型可见 `Content` 限制在约 32KB，完整原文进 `RawContent` / archive。这是写入时策略，不是阈值触发的历史 snip 投影。

## 六、Provider 与输出预算

- 应用层 summary 是默认路径；Responses 等 native compaction 标记 unsupported 时回退 summary
- `max_output_tokens=0` 表示 auto ladder（普通 16K / reasoning 32K / high·max 64K）；128K 仅显式配置
- auto ladder 与 `compact_ratio` 解耦

## 七、缓存影响

| 场景 | 预期 |
| --- | --- |
| warm resume 低于阈值 | 复用 append-only 前缀，无摘要 |
| 首次跨过 compact_ratio | 前缀变为 prefix+summary+tail，一次预期 miss |
| checkpoint 安装后继续对话 | 稳定 prefix 利于 hit；generation 作用域避免重复摘要 |
| cold resume | 只记 cache 状态，不因 TTL 重写历史 |

## 八、验证与烟雾

- 确定性：`internal/agent` compact / projection / prune no-op / restart 测试
- 离线 e2e：`benchmarks/context-maintenance-e2e` 的 `seed` + `resume`（`-offline`）
- 在线 e2e：同目录 `continue`（`DEEPSEEK_API_KEY`，`-max-usd` 费用上限，至多一次摘要）

## 九、有意保留的兼容层

不算功能缺口，也不声称“代码里已无旧概念”：

1. 配置结构体仍可读旧 soft/snip/force 键，加载时清零并迁移删除
2. sidecar 仍可解码旧 prune/native 字段后忽略
3. `PruneStaleToolResults` / `SnipStaleToolResults` 保留为 no-op API，避免旧调用点 panic
4. snip 几何 helpers 仍服务 first-visible 与 summary fold 输入，不再用于自动投影安装

## 十、明确未做

1. 重新启用多阈值自动 prune/snip 投影
2. 把 cache TTL 重新绑定到 transcript 改写
3. 跨 session 的 EventChain L2 自动恢复作为维护主路径
4. 完整 break-even 成本 dashboard
