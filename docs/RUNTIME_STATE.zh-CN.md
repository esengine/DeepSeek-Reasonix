# 会话运行状态

[English](RUNTIME_STATE.md)

Desktop、Serve 和远程会话使用控制器提交的运行状态快照。正文和完成事件仍由原来的 transcript 链路处理，运行状态同步不会增加模型请求、重载历史或更改会话/inbox 持久化格式。

## 界面行为

| 状态 | 会话与项目指示 | 发送控件 |
| --- | --- | --- |
| 正在执行 | 按实际活动显示思考或输出 | 保留已有发送、停止语义 |
| 正在收尾 | 静态“正在收尾”，不再显示思考动画 | 隐藏本轮停止；新输入持久化排队 |
| 等待确认 | 显示待确认 | 保留原确认入口及保护 |
| 正在取消 | 继续显示取消中，直到控制器结束 | 不重复发送取消 |
| 后台任务 | 显示任务数量；非当前会话也计入项目 | 不把后台任务当作前台模型执行 |
| 状态待同步 | 保留最后已知事实，以静态不可确认状态显示 | 暂停发送和停止，恢复连接后重新确认 |

收尾时入队成功显示“已排队”并清空输入；失败保留输入并显示错误。相同草稿重试使用原幂等键。远端 POST 结果不明时只查询该会话、该键的 receipt，不自动重发写请求。远端队列快照按会话和连接身份校验，接续任务开始后会更新条目状态。

项目指示汇总该项目所有运行实例。同一远程会话通过多个标签显示时只计一次。后台任务被取消但尚未实际退出时，仍保留计数及原有资源保护。

## 协议与边界

`event.RuntimeStateSnapshot` schema version 1 显式包含：

- `runtimeEpoch`、`revision`：运行实例代次和单调版本；同版本对应同内容。
- `phase`：`idle`、`executing`、`finishing` 或 `closed`。
- `running`、`turnId`、`turnStatus`、`turnEventSeq`。
- `pendingPrompt`、`cancelRequested`、`cancellable`、`backgroundJobs`、`activity`。

旧 `Running()` 的执行/收尾保护语义保持不变。控制器在提交边界生成快照，经有界、离锁通知发布；后台任务在真正关闭 done 后发布最终计数。通知不写入 WAL、transcript 或 provider 消息。

Desktop 的 `GetRuntimeStateSnapshot` 和 `runtime-state:changed` 使用相同完整投影，包含投影 epoch/revision、sessions 和 topics。本地采样离开 App 锁读取控制器后，复验标签、控制器、会话代次、路径及打开/分离身份。旧项目树接口由同一投影适配。

Serve 的 `GET /runtime-states` 仅读取前台及 detached 控制器的内存快照；`/status` 增加 `runtimeState`。指定 session 时优先匹配真实拥有该 session 的实例。SSE 的 `runtime_state` 使用既有 session tagging，新的会话状态不会越过 `session_changed` 屏障。外部接管和只读镜像继续遵守原 ownership 规则。

远程 reducer 同时处理 GET/SSE，保留连接 generation、client、selection 和路由保护。旧版本被丢弃，同版本同内容为无操作，同版本冲突触发合并后的重同步。新 epoch 需要权威读取确认；晚到 GET 不能覆盖更新的 SSE 或新绑定。

## 同步与兼容

应用先订阅、后读快照。正常变化立即推送；应用级 owner 每 30 秒校验，每个 Serve 连接在一次校验中只读取一次。挂接、焦点恢复和连接变化触发即时校验，在途请求合并。失败按 5、10、20、30 秒退避；失败不清空已知状态。

新快照可用时，旧的逐会话运行 watchdog 不再判定运行真值，十分钟沉默清理也不再决定项目活动状态。旧 Serve 的 404/501 能力缺失在当前连接代次内记住，使用原有状态接口；缺失字段不作为零值，也不猜测收尾阶段。所有旧协议字段保留。

诊断仅记录状态来源、匿名代次、版本、阶段、同步原因及丢弃/冲突/失败计数；不逐 token 输出，不记录提示词、凭据或完整路径。

## 验证入口

- 根模块：`go test ./...`；`go test -race ./internal/control ./internal/jobs ./internal/event ./internal/serve`。
- Desktop 独立模块：`go test ./...`；`go test -race . -run 'RuntimeState|RemoteRuntime|ProjectTreeRuntime|SessionRuntime|RuntimeBinding'`。
- 前端：`runtime-state-store.test.ts`、`composer-inbox-recovery.test.tsx`、项目树专项、`pnpm test:remote`、`pnpm test:app-lifecycle`、`pnpm build`。
- 浏览器：`node bench/runtime-state.mjs` 使用真实 Chromium 与可控帧覆盖收尾、排队、失败重试、后台任务、远程断线/恢复及会话切换；`pnpm test:app-browser` 覆盖常规发送和界面生命周期。浏览器帧夹具不能替代控制器及 HTTP/SSE 集成回归。
- 原生桌面需另外验证。macOS 的本地隔离应用和 loopback 测试模型可验证真实 Wails 发送、完成及侧边栏清除；Windows/Linux 不能由浏览器结果推定通过。
