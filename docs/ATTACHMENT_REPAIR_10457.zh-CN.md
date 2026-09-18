# 附件接纳与请求隔离修复（#10457）

[English](ATTACHMENT_REPAIR_10457.md)

附件改造来源为 PR head `b582ebabe089fb75a6ee8ba23dddcb0a48a873ca`，先语义重放到
`main-v2` `93fb72870758f2ff60ae24386406b0e5f0782f0e`，再变基到最新主线
`9b0cc56e5c0e60d92cb447b360ef8fa018c7c72b`。初始基线已包含 #10469
的持久草稿和 #10471 的固定快照导出，最新基线还包含 #10500 的模型级 reasoning
能力解析。相对上一版 `bd0733b9` 的 `git range-diff` 仅变化了生成的 Desktop
契约摘要，附件补丁与后续主线行为均保留，也未整合 #10346。
借鉴 harness `ddefc45fbc7f8e46dd73185e68295696d1297887` 的批次接纳、
会话授权和独立取消机制，保留 Reasonix 原图字节、内容库与现有请求策略。

## 用户可见行为

显式图片是提交的一部分。整批图片先读取、解码、验证并保存原图，再接纳轮次。
任一失败均不调用模型或工具、不追加成功用户消息、不执行 Goal setup，输入框文字和
附件保留。失败期间已保存但未引用的内容对象可以保留，不能为回滚而删除共享原图。

已接纳的轮次持有冻结引用，不再依赖源文件或草稿凭证。请求身份使用版本化指纹，
区分用户的逻辑附件与临时传输凭证；重试先查持久回执，源文件删除后也不会重复执行。
延迟执行的闭包在进入排队前捕获图片，不异步读取提交阶段的临时共享字段。

附件操作开始前固定目标令牌，包含标签页、运行时、会话存储身份和工作区。
切换焦点不会转移操作，关闭或替换目标会使旧令牌失效。同一会话重建运行时后保留原
草稿归属证明；用户重试时重新校验并续签凭证，重复续签返回同一结果，不自动发送。
清理旧凭证会撤销对应的凭证家族。成功回执仅清空同一草稿版本，发送期间的新输入保留。

控制器先创建、会话后发布时，附件内容库也由会话发布方绑定到真正的持久化内容图，
不能固定在旧兼容目录。`attachment_owner_test.go` 复现这一构造顺序，并验证运行时
替换后仍能读取原图和续签草稿。

## 九项问题的修复与证据

| 问题 | 修复机制 | 验证 |
| --- | --- | --- |
| 接纳后模型没有图片 | `ImageInputs` 贯通普通、编辑、Goal、调用、队列；在协议规范化之前解析 | 控制器入口测试、真实 boot 子 Agent、loopback provider 捕获 |
| 工作区读取越界 | `os.Root` 受限读取、拒绝末级链接、打开前后身份校验、Unix 非阻塞打开 | 父目录链接、工作区归属、两种权限一致性 |
| 导出漏图或破坏已有目标 | 扩展 #10471 `ExportSnapshot` 的授权有序图片闭包和任务私有渲染；继续使用其不覆盖发布及恢复日志 | `sessionexport/build_test.go`、会话附件闭包及 #10471 发布测试 |
| 队列依赖临时凭证 | 保存完整图片引用与来源别名；执行前检查内容库；编辑先准备后替换 | 源文件删除后执行、损坏阻塞、混合来源编辑 |
| 重试重复执行 | 接纳前和门闩内查回执；逻辑身份指纹排除可变凭证 | 释放凭证后重试、不同凭证相同逻辑身份 |
| 多来源绕过限制 | 结构化、旧路径与授权历史来源合并后统一校验 | 多图部分失败、20/21 张、64 MiB 与 200 MiB 边界 |
| 服务/草稿生命周期错误 | owner 初始化服务、生命周期上下文、目标令牌、验证后续签 | Desktop 焦点切换、运行时替换、取消、Composer |
| 取消污染共享变体 | 最后等待者离开时在锁内移除并取消；旧任务只清理自身 | 屏障控制的独立取消和取消后继任务，辅以 race 检查 |
| 使用错误模型或账户 | 路由绑定实际主模型、视觉理解模型或子 Agent；贯通轮次 ctx | 上传参数拦截、文本转视觉、真实 boot 子 Agent |

对应测试位于 `internal/control`、`internal/attachment`、`internal/boot`、
`internal/session` 和 `desktop`。验收断言 provider 实际收到的图片，而不仅是准备器返回
引用。工具先保存原图，随后由实际请求路由理解；保存失败保留工具文本和诊断，避免重复
执行有副作用的操作。

## 兼容与提示缓存

保留 `.content-v1`、revision 3、inbox schema 3 和旧 `Images []string`，不批量重写历史。
OpenAI、Responses、Anthropic 三种协议通过 loopback 捕获真实请求体，逐字节比较旧图片
历史进入解析器前后的一致性。系统提示、工具 schema 和历史文本顺序不变。

新图片继续使用确定性 policy v1，变体缓存上限 512 MiB；原图校验和编码共用宿主最多
两个重型任务的限制。新图片真正进入请求后，相比之前缺图的请求发生首次缓存未命中属
预期变化。未知回执版本或无法验证的旧回执明确拒绝自动重放。

`attachments-v2` 目标操作能力不足的远端明确返回不支持，不跨工作区降级。schema
兼容测试证明不支持 revision 3 的读取方会明确拒绝，不执行原地降级；上一版本真实二进制
验证属于独立的发布验收，不能由源码测试推断。

## 检查和测试包复现

验收按风险分为三层。

第一层是 PR 必须门禁，每次代码修改后运行，失败即不推送。它覆盖附件、提交、队列、
历史预览和变体缓存的聚焦 Go 测试，确定性交错及相关 race，Desktop 与 Composer 附件
测试，typecheck、契约 freshness、`repolint`、inventory、preview bundle budget，实际
provider 图片摘要，以及显式图片失败时 provider、工具和 Bash 调用均为零。OpenAI、
Responses、Anthropic 三类协议的旧 `Images []string` provider 可见字节也必须保持不变。
共享代码大幅变化时执行一次 root、Desktop 和前端全量；后续仅改文档、mock 或分包时，
只重跑受影响套件和静态门禁。

与附件补丁 patch-id 等价的集成候选已完成 root 和 Desktop 全量 Go、相关 race、全部
409 个前端套件、typecheck、正式构建与包体积门禁、契约 freshness、inventory 和仓库
lint。最终变基通过 `range-diff` 保留附件语义，并重跑受影响的模型路由、Desktop 和
前端门禁。Desktop Linux cross-lint 仍报告 `main-v2` 基线中 4 个未改动的 tray 桩函数，
不属于附件回归。

第二层只在准备推送的最终 SHA 上执行一次。使用 `scripts/desktop-build.sh` 生成 ZIP，
解压到全新目录；不要运行尚未组装 CLI/service 的中间 Electron 壳。随后执行：

```sh
node desktop/packaging/smoke.mjs /isolated/Reasonix.app
node desktop/packaging/attachment-native-smoke.mjs \
  /isolated/Reasonix.app /isolated/evidence
```

自动验收启动正式打包的壳和 service，使用独立数据目录与本机 loopback provider，检查
进程目录与工作区不同时，工作区权限附图仍成功；`view_image` 与自动图片输入一致；取消
后重试不重复创建轮次；图片读取失败保留真实 Composer 草稿。测试输出 JSON 与截图，
不安装或覆盖任何应用。

来源分支曾基于 `86fd76f20d7d` 生成隔离预览包并留下附件 loopback 证据；这些只作为
历史输入，不构成当前集成 head 的验收。发布候选必须针对自身 SHA 重新执行常规包体
smoke 与 `attachment-native-smoke.mjs` 后才能声明打包运行时通过，生成证据继续不纳入 Git。

第三层只在风险触发或正式发布时运行。存储 revision 或 inbox schema 变化需要上一版本
升级、降级和安全拒绝测试；签名、Electron 壳或 service 布局变化需要完整原生启动矩阵；
权限模型变化需要工作区权限与完全权限对照；provider、Files API 或上传缓存变化需要真实
provider 和多账户隔离。本 PR 保留 revision 3、inbox schema 3，也不修改 Files API 或
上传缓存，因此上一版本真实二进制与真实网络上传验证均为“不适用”。

loopback 证明图片传输、路由和权限行为，不代表付费远端模型的语义识图质量；测试无需
读取真实凭证。导出崩溃恢复发生在重新对同一目标发布时，不在应用启动时扫描任意导出目录。

本次修复不合并、不发布、不覆盖已安装应用，不调整 Bash 搜索，不增加上传缓存或原图
垃圾回收。
