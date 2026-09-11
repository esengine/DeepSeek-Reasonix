# 工具中断后的持久恢复

执行证据由 Go runtime 管理。Electron 本地 tab 与 Remote tab 共用同一套
Controller API，沿用 session、turn ledger 和文件 checkpoint，不新增恢复数据库。

## 执行边界

参数检查、工具目标解析和权限判定完成后，才建立调用回执。回执包含 session、
turn、call、attempt、规范化参数摘要和幂等键，先通过现有 session checkpoint
持久化，再同步落盘 `tool_started`，最后调用工具。第一个写工具同时把身份和
transcript 摘要绑定到文件 checkpoint。

返回结果后，更新同一 attempt 并保存。显式失败、未开始、运行后取消或超时分别
处理；“调用过”不等于“外部效果已确认”。重启后，已开始但未确认结果的调用仍为
未知；新格式中只有 dispatch、没有 start 的调用可以认定未开始。旧记录缺少证据
时保守处理，不猜测成功或失败。

未知写操作阻止后续写操作，即使模型换了 call ID；只读诊断可以继续。同一 session
的压缩或历史改写保留未解决回执。用户确认记录为 `user_confirmed`，不会伪造工具
成功输出，也不会自动再次执行已确认的相同工具和参数。

## 界面与传输

桌面统一入口是 `GetToolRecoveryForTab` 和 `ResolveToolRecoveryForTab`。
Remote tab 转发到 Serve 的 `GET/POST /tool-recovery`，沿用认证、会话路径和
前台写权限保护。

快照携带 `sessionPath`、`runtimeEpoch`、内容修订号 `revision`、调用列表和重试
能力。操作必须提交该快照及 `attemptId`、`inspectionId`；运行中、结束处理中、
会话切换中、已关闭、旧 epoch 或旧 revision 均拒绝。

恢复卡支持检查、确认已生效、不重试和受限重试。普通查询隐藏原始参数，显式检查
才返回本地参数回执。检查结果区分效果存在、文件后置条件满足、效果不存在且旧
尝试已被阻止提交、无法确认。文件后置条件只证明当前状态，不证明原调用结果。

确认必须针对已检查的同一 attempt。不重试会保留未知事实与写入屏障；全部解决
后，继续任务走现有普通回合提交。切换 tab/session 后，旧异步响应不得覆盖新界面。

## 重试与幂等

默认关闭重试。只在拥有该 session 的 Go 主机设置
`REASONIX_TOOL_RECOVERY_RETRY=1` 才开放重试操作，远程能力由远端主机决定。

重试使用新 call ID 和 attempt ID，保留原幂等键和原始参数，经过正常的参数、权限、
hook、租约和工具执行管线。旧请求不能再次提交同一重试。

只读工具重试要求旧执行者已经退出。写工具必须实现 `tool.EffectVerifier`，并在
重试前重新证明“效果不存在，且旧尝试已无法再提交”。仅观察到不存在不够。
验证器的 `RecoveryScope` 必须与原记录的接收端、账户和资源身份一致。
工具可读取 `tool.RecoveryIdempotencyKey(ctx)`，在实际接收端实现去重。

文件工具复用已有 `WriteVerifier` 做只读检查。通用 shell 和任意 MCP 服务无法
自动证明外部效果不存在，因此默认保持未知。本实现不对没有权威回执或去重能力
的第三方服务承诺 exactly-once。

## 协议与兼容

transcript gate 与执行恢复独立：在 provider-request interceptor 之后、请求发送
之前，验证 adapter 配对规范化后的视图。已被 host 确认未执行的坏参数调用，可以
在请求修复视图中使用空对象，保留错误说明；本地原参数不改写。正常请求字节不变，
恢复回执不进入系统提示、工具 schema 或模型消息。

新字段为可选本地元数据，旧 JSON 可读、旧事件编号不变；旧远程客户端仍受新服务端
的执行屏障保护。旧版本可执行程序没有新恢复保证，因此应先解决未确认效果再降级
Go runtime；本功能不自动改写存储格式或执行降级迁移。

快照还提供从保留的 session 证据计算的未知、已确认、重试、拒绝和阻断计数，
不把这些数据当作历史全量遥测。

## 验证与交付

测试覆盖权限/参数拒绝、开始前持久化失败、状态分类、快照隔离、旧 attempt 拒绝、
幂等接收端、重复重试、存储失败后的确认回滚。真实子进程在副作用 fsync 后、结果
返回前退出；重启和并发确认验证结果持久化，并确保副作用只发生一次。

浏览器验收命令：`cd desktop/frontend && node bench/tool-recovery.mjs`。
覆盖检查、确认、继续、禁止不安全重试、切换 session 后拒绝迟到响应。

部署时配套更新 Go runtime 与 Electron 生成契约，先保持重试关闭；核实具体工具的
接收端语义后再开启。关闭重试不会删除证据或人工恢复能力。远端发布和生产灰度是
独立交付步骤，不等同于本地实现与测试完成。
