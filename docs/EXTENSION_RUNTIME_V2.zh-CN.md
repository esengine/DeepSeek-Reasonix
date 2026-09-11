# Extension Runtime v2（时空可组合性）

Reasonix 插件/运行时 v2 模型说明。英文版：[EXTENSION_RUNTIME_V2.md](./EXTENSION_RUNTIME_V2.md)。

## 协议与 Manifest

- 插件清单：仅接受 `apiVersion: reasonix.io/plugin/v2`（拒绝 v1 与无 apiVersion 的 native）。
- 兼容边界：不提供 v1 双读或自动迁移。扩展 Manifest v1 从未公开发布，
  因此 v2 是首个受支持的 Runtime Manifest。
- 扩展线协议：`reasonix.extension.v2`（major 2）。
- Handshake 的 `provides` 不得超过 manifest 的 provides 上限。

## 运行时模型

```text
RuntimeSnapshot = 不可变配置与依赖视图
RuntimeSet / EffectScope = 一代 live 资源
RuntimePlan = 旧 → 新 的转移（子图分类）
RuntimeOwner = 单个 session lineage 的 gate + receipt + stream/file 证据
Controller = 已发布 generation 的消费者（admission 绑定 RuntimeOwner）
```

组件状态：`Inactive → Preparing → Active → Draining → Inactive`（或 `Failed`）。

## 重载

- 优先 `boot.RebuildFrom(previousBuildResult, opts)`。
- no-op / interceptor / UI / provider / MCP-only 走 **真子图 patch**（不进 `BuildRuntime`）；`ReusedController` 时调用方不得 `Close` 旧指针。
- Provider/MCP 子图：`WithLiveContributions` 刷新 interceptor/provider/UI 目录；**system prompt + tool schemas + CacheHash 保持稳定**（只滚 backend）。工具 schema 改名仍需全量 rebuild。
- 窄路径为 **stage → ready → commit**（失败原子）：
  - **Stage**：启动/收养 sidecar，构建下一代 dispatcher/resolver；**不**安装 stream router，也**不**对 UI hub 调用 `BindGeneration`。
  - **Ready**：等待 sidecar 就绪。
  - **Commit**：安装 stream router、绑定 UI generation、替换 controller 绑定，再 publish。
  - stage/ready/commit 任一步失败：`RollbackPlanStart` 回挂 Unchanged 客户端；stage 前的 stream router 继续消费 `stream/chunk` / `stream/end`。
- **Stage 期间的 UI**：在 commit 之前仍绑定旧 generation。sidecar 在 handshake/ready 期间若用**下一代** generation 发 `host/ui/publish` 或 `host/ui/request`，会被当作 stale **丢弃**。协议约定：在 runtime generation 发布前，不要依赖 UI 可见性。
- 迁移成功后：**发布** 新 generation，再 **排空** 旧 sidecar。
- Drain 超时先 cancel 已注册工作（被替换的 controller、**host provider stream**（`HostStreamRegistry`）、extension provider stream、StableProxy/MCP 在途），再写 `drain-timeout` receipt。
- Draining 中的 Controller 拒绝新 turn（`turnDroppedDraining`）。

## 诊断

```bash
reasonix doctor runtime
reasonix doctor runtime --json
reasonix plugin doctor <name>
```

输出组件状态、计划、effect receipt、可恢复性、lifecycle metrics，以及进程内的
`runtimeOwnerFallbacks` 计数。产品启动路径会绑定独立 owner；该值非零表示仍有
兼容路径落到了共享默认 owner，需要补齐显式接线。
计划诊断拆分两个事实：`prefixChanged` 在构建完成后比较新旧 snapshot 的
`CacheHash` 得出；`providerChanged` 表示 Provider capability 的新增、删除或
重载。因此仅滚动 Provider backend 且 system prompt、tool schemas 字节不变时，
诊断为 `prefixChanged=false, providerChanged=true`。

## Effect receipt

不可逆外部动作记入当前 `RuntimeOwner` 的 receipt store。独立 session
不共享 publish/drain 状态或恢复证据。Recovery **不得** 对 irreversible
声称 rollback 成功；使用 owner 级的 `AssessRecoverability(generation)` /
`DecideResume(generation)`。

Receipt ledger **只支持进程内 rebuild/resume**，不会持久化；进程崩溃后的恢复
不在本次 Runtime v2 的范围内。内存最多保留最近 32 个 generation、每代 256 条
receipt。淘汰时也会释放关联的文件 prior 字节，并将该代证据标记为已截断；证据
不完整时，诊断不会声称 clean rollback。
消息发送去重与 receipt 使用同一保留周期：消息 receipt 被淘汰时会同步释放对应的
`(generation, messageID)` 键，避免形成第二份无界账本。文件 prior 每条最多 8 MiB、
每个 `RuntimeOwner` 合计最多 32 MiB；超过任一上限会记录 `prior_truncated`，恢复
判断按保守策略处理，不会声称 clean rollback。

- Provider stream open 会记录 `provider-submit:<id>`（不可逆）。
- 已完成的 Provider stream 会注销 drain 回调；generation gate 只保留仍在途的 stream。
- Drain 超时 force-expire 记录 `drain-timeout:<gen>`。`ScheduleDrainWatch` 仅在确有
  generation 正在 drain 时启动，并将快速连续 publish 合并为每个 owner 一个 watcher；
  doctor sweep 仍作为兜底。
- 每个 owner 最多保留最近 256 个 late-cancel 过期 generation 标记。

## EffectScope 归属

一代运行时的 live 资源挂在 `RuntimeSet` / `EffectScope`：

| 资源 | 跟踪方式 |
| --- | --- |
| Sidecar manager | Activator 中 Cancelable effect |
| UI hub 绑定 | `TrackUIHub` |
| MCP plugin host | 清单 + `session-resources` dispose |
| LSP manager | `TrackWatcher` 清单 |
| Session cleanup 链 | `TrackControllerCleanup` |
| Provider 已提交 | `RecordProviderSubmit` receipt |

## 验收对照

| 规格验收项 | 状态 |
| --- | --- |
| 每个资源与 session lineage 有明确 owner | 完成（`RuntimeOwner` + sidecar/MCP/UI/LSP/stream/file receipt 接线） |
| 激活失败不泄漏 / 不 publish | 完成 |
| 缺依赖 → Inactive 诊断 | 完成（结构化 missing requirement + Unavailable） |
| 真子图 rebuild（不进完整 BuildRuntime） | 完成（None/Interceptor/UI/Provider/MCP） |
| Publish/drain 顺序 + drain 取消 | 完成 |
| 不可逆永不标 rollback 成功 | 完成（recovery `AssessRuntimeResume`） |
| 原生 Runtime Manifest 严格 v2-only | 完成（无 v1 双读 / 自动迁移） |
| 缓存稳定性守卫 | 完成 |
| Doctor 解释 inactive + resume | 完成（CLI + desktop RuntimeDoctor UI） |
| 无外部运行时依赖 | 完成 |

## Phase 5 决策

- **兼容性**：v2 是首个受支持的扩展 Runtime Manifest；install/doctor/boot
  不双读 v1，也不自动迁移。
- **性能**：见 [EXTENSION_RUNTIME_V2_PERF.zh-CN.md](./EXTENSION_RUNTIME_V2_PERF.zh-CN.md)。
- **README / SDK**：示例 manifest 在 `sdk/go/examples/*`；wire 类型以 protocol gen 为准。
