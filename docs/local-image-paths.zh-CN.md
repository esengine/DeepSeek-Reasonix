# 按路径读取本地图片

向智能体提供可访问的本地图片路径，并要求查看图片。内置 `view_image` 工具读取 PNG、JPEG、GIF 和 WebP，返回结构化图片内容。优先交给当前视觉模型；当前模型不支持图片时，复用设置中的专门识图模型生成摘要。未配置识图模型时明确提示图片未被理解。单独写出路径不会上传图片。

相对路径以任务工作区为基准，也支持会话外部文件夹别名。沿用敏感文件和禁止读取目录规则，包括符号链接的目标。仅接受普通文件，最大 3 MiB、4000 万像素；根据内容识别格式，而不是文件扩展名。

## Composer 附件

粘贴或拖入的文件先保存在发起操作的标签页工作区中的 `.reasonix/attachments`，再作为不可变原图写入会话内容存储。保存、预览和原生视觉读取在异步操作期间始终绑定同一个标签页及工作区；切换标签页不会把结果写入新激活的草稿。目标绑定操作会在浏览器读取文件或计算摘要前协商 `attachments-v2`；旧标签页 API 保留 `attachments-v1` 能力名，两条路径都不信任客户端自报摘要。历史 `@.reasonix/attachments/...` 引用无需改写即可继续读取。已接纳图片的历史卡片只带 digest；桌面通过 `ReadSessionAttachmentForTab` 在会话内容图授权后再加载原图。远程 Serve 不声明这两项本地暂存能力。

显式图片附件会在轮次被接纳及草稿被清空之前完成读取、完整解码和持久化。图片缺失、不可读、路径不安全、格式不支持、损坏、超限、取消或读取期间发生变化时，整轮会在模型和工具启动前被拒绝。Composer 保留文字和附件，用户可恢复文件或重新添加后重试。普通非图片引用保持原有行为。接纳之后，原工作区文件被修改或删除不会改变已持久化的原图。

新消息写入有序的 `image_inputs`（内容寻址附件引用、外部 URL 或 Files ID）。旧的 `images []string` 仍用于读取历史 data URL、HTTP URL 和 Files ID。同一条消息不得同时携带这两个字段。会话 `StorageRevision` 为 3，队列 schema 为 3：本版本可读旧数据；旧版本拒绝读写新会话格式，并对新版队列保持只读暂停。

OpenAI Chat 和受支持的 Anthropic 适配器已有工具图片转发。Responses 现在会在整组工具结果之后追加图片内容，保留调用与结果顺序。非视觉模型不会收到图片载荷。官方 DeepSeek 视觉 SKU 支持工具图片：Chat 和 Responses 在整组工具结果后追加用户图片，Anthropic 使用顶层用户图片块。普通 Flash/Pro 通过已配置的识图模型理解摘要，不接收原始图片。Files 上传发生在实际模型请求准备阶段，使用该请求的上下文，不会回写历史。

升级后，新增 `view_image` 会一次性改变稳定工具定义前缀，可能导致首次提示词缓存未命中。工具定义不会逐轮变化。旧历史图片保持原来的 provider 可见字节。新附件使用确定性请求变体（最长边 1568 像素，策略版本 1），升级后的第一轮可能未命中提示缓存。

所有结构化工具图片（包括 MCP 和按需 MCP）共享识图服务。`vision_model=auto` 只选择当前服务商的视觉模型。摘要标记为不可信上下文并注明来源；同一会话按内容缓存，未验证内容的 URL 不复用摘要。识图失败不重新执行原工具，也不丢弃原工具文本。

## 兼容

| 数据 | 旧版写入 | 本版本 | 上一版本 |
| --- | --- | --- | --- |
| 旧 `images []string`（data URL / HTTP / Files ID） | 写入 | 原样读取，不批量改写 | 原样读取 |
| 新 `image_inputs` 附件引用 | 不写入 | StorageRevision 3 后写入新消息 | 拒绝会话（`ErrUnsupportedVersion`） |
| 工作区 `.reasonix/attachments/...` 文件 | 写入 | 仍是 Composer/CLI 入口；接纳后的对象在 `.content-v1` | 按文件读取 |
| 队列 schema 2 | 写入 | 写打开时升到 3 | 可读可写 |
| 带 `imageInputs` 的队列 schema 3 | 不写入 | 写入 | 只读暂停 |

## 验证

确定性测试覆盖取消、并发缓存复用、工具消息摘要恢复、子任务隔离、按需 MCP，以及识图失败时保留原工具执行结果。所属包可运行 `go test ./internal/attachment ./internal/imageinput ./internal/agent ./internal/control ./internal/session ./internal/sessioninbox ./internal/tool/builtin`。导出/导入会走遍旁路 `.inbox` 对象，缺失内容寻址对象时拒绝。旧版读者拒绝 StorageRevision 3，并对队列 schema 3 只读暂停。

附件回归还覆盖进程目录与工作区不同、不同工作区存在同名图片、读写期间切换标签页或替换运行时，以及缺失、损坏、越界、符号链接、超限和多图部分失败时的原子拒绝。确定性回归会在“工作区内修改”和“完全权限”下读取同一图片；自动图片输入与 `view_image` 都不依赖切换进程目录。

显式开启的实测使用随机短码和彩色色块生成图片，读取已配置的官方 DeepSeek 凭据但不打印凭据，验证三种原生协议及显式、自动摘要服务路线，并更换图片检查新内容：

```sh
REASONIX_LIVE_TOOL_IMAGES=1 go test -tags live ./internal/imageinput -run TestLiveToolImages -v -count=1
```

实测的 `auto` 用例注入同服务商选择器以验证服务路线；真实配置目录的选择由 Boot 负责。实测会产生 API 用量。请求成功不等于读图成功，必须通过图片内容断言。OCR 仍可能误认相似字符，摘要不能替代逐像素的视觉输入。
