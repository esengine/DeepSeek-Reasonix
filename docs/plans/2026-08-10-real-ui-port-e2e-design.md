# intelifar 真实 UI 默认端口 E2E 设计

## 目标

将 `4388` 从一个只存在于启动实现和说明文档中的默认值，提升为可持续回归的真实 UI 契约；同时补齐此前因机器缺少 Go 而未执行的上游 Reasonix 测试。

## 方案选择

端口契约采用独立的 CLI 黑盒 E2E。测试使用 Node 子进程直接运行 `server/real-analysis-server.mjs`，显式移除继承的 `PORT`，等待启动日志后访问 `http://127.0.0.1:4388/`。浏览器验证 intelifar 首页、IP 全景图和受控 IP 任务助手，HTTP 验证 `/api/health` 的 `real` 模式与 MinerU、DeepSeek 配置状态。该测试只使用占位密钥，不调用外部供应商，避免把端口回归与 MinerU 排队或 DeepSeek 波动耦合。无论成功或失败，测试都关闭浏览器并回收网关子进程。

备选方案一是把现有 MinerU + DeepSeek 真调用 E2E 固定在 4388；它覆盖面高，但耗时长且受外部服务影响，不适合作为每次 `verify` 的端口门禁。备选方案二是只读取源码或调用健康接口；它无法证明 CLI 默认值、静态 UI 和浏览器路由共同工作，因此不满足 E2E 要求。

## Go 验证边界

仓库包含三个 Go 模块：根 Reasonix 模块、`sdk/go` 和 `desktop`。使用 `go.mod` 指定的官方 Go 1.26.5 Windows amd64 便携包，校验官方 SHA-256 后解压到用户缓存，不修改系统 PATH。分别执行根模块 `go test ./...`、SDK `go test ./...` 和 desktop `go test ./...`。如果出现平台限定、外部工具或真实代码失败，保留原始失败信息，不把它误报为“未执行”。

## 验收标准

- 默认 CLI 启动日志和监听地址均为 `127.0.0.1:4388`。
- `/api/health` 返回 HTTP 200、`mode=real`，两项 Provider 均为 `configured`。
- Chromium 能打开首页、`#assets` 和 `#agent`，对应主视图可见。
- 新 E2E 被 `npm run verify` 和 `npm run verify:real` 调用。
- 三个 Go 模块都有实际测试结果，或记录到具体包/用例级的真实失败。
