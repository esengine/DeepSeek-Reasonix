# 聊天内容与交互宿主

## 目标与来源

当前聊天实现借鉴 DeepSeek Harness `c291e7961a515f6d7af9304e7fd1d257929aef26` 的内容分层、过程摘要、工具行、调用检查和文件产物交互，并适配 Reasonix 已有协议和工作台。生产路径只有一套实现：本地会话、远程会话和已有工作台都消费统一投影。

```text
controller / 历史 / LiveStream / 远程事件
                    ↓
       ChatSource 内容与轮次投影
                    ↓
   聊天正文 / 过程摘要 / 轮次文件
                    ↓
工具详情抽屉 / Workspace dock / 浏览器 / 交互宿主
```

投影是可重建的视图数据，不是第二份持久化会话事实。Reasonix 没有引入 Cordis、另一套服务端事件日志或 Harness 的整个 Conversation 框架。

## 已交付的产品行为

| 内容 | 默认展示 | 用户操作 |
|---|---|---|
| 用户消息 | 文字、附件、时间、复制 | 图片和文件进入已有资源宿主 |
| 助手回答 | 完整 Markdown | 代码、表格、公式、图片和链接使用各自 renderer |
| 思考 | 单行摘要和耗时 | 展开正文；大内容通过共享全文加载器读取 |
| 工具 | 类型、说明、状态的紧凑行 | 行内展开；详情抽屉查看结果、参数、子调用和原始记录 |
| 明确展示文件 | 成功内置 `present` 的文件卡，默认前 4 张 | 预览、源码、浏览器、文件树、复制路径、保存副本、系统操作 |
| 本轮修改文件 | 成功内置文件工具产生的紧凑条目，默认前 6 项 | 使用同一资源操作入口打开或保存 |
| 失败和未知内容 | 局部状态与安全兜底 | 查看可用原文；不使整个轮次崩溃 |
| 轮次尾部 | 复制、普通分支、用量、用时和时间 | 沿用当前会话能力检查 |

保留“仅可查看／工作区内修改／完全权限”、输入框、发送停止、提问审批、模型设置和项目导航。没有增加赞踩、聊天内回退、交付验收或展示模式开关。

## 稳定节点与轮次

`ChatSource` 继续提供顺序、节点和状态三组独立订阅。顺序未变化时复用数组；节点未变化时复用节点快照。流式增量只更新目标助手节点和受影响轮次，落盘结果替换同一节点，generation 和 revision 只用于丢弃过期结果，不进入 React key。

节点身份来自已有消息 ID、调用 ID、父调用 ID和轮次边界。跨页缺少完整边界时保留不完整状态，不使用“最近一个工具”或数组下标补造关联。历史 prepend 增加节点并修补索引，replace 才整体重建。

成功结束、有最终回答且过程边界完整的轮次自动收起。运行中、取消、中断、最终失败、只有工具或半轮历史保持展开。可恢复工具错误允许随成功轮次收起，但摘要保留失败数量。用户手动展开后，后续流式更新不会再次强制收起。

收起过程会卸载其 Markdown、图表、终端输出和工具详情子树。它不采用 Harness 的 `hidden="until-found"` 隐藏 DOM，因此浏览器查找只覆盖当前已挂载内容。

## 工具 renderer 与详情

工具分类按可信内置身份匹配：`present`、web、shell、agent、文件和通用兜底。插件或 MCP 工具仅凭名称包含 `write`、`edit` 或 `present`，不能获得内置文件能力。

Shell 标题优先显示实际执行器：PowerShell、Git Bash、Bash、Zsh、Shell；无法确认时显示 Terminal。行内 renderer 复用当前终端、网页来源、文件差异和通用结构化输出。

详情抽屉按稳定调用 ID 订阅，按数据存在性显示以下标签：

- 结果：输出、错误和差异。
- 参数：格式化参数，流式或未知参数保持可复制原文。
- 子调用：父子调用导航。
- 原始记录：当前客户端实际拥有的调用记录，不声称是完整服务端轨迹。

大结果通过 `ChatContentLoader` 去重和限流。关闭抽屉、切换目标或切换会话会使旧请求失效；全文复制只有在完整内容加载成功后才完成。

## 文件事实

文件投影区分明确展示文件和本轮写入文件：

1. 只有成功、无错误且名称精确为内置 `present` 的结果才能形成展示卡。
2. 只有成功的内置 `write_file`、`edit_file`、`multi_edit`、`notebook_edit`、`delete_range`、`delete_symbol` 和 `move_file` 能形成修改条目。
3. 读取调用、失败／拒绝／取消调用、`no changes made`、不完整参数和普通 Shell 不形成文件事实。
4. 同一文件若已经由 `present` 明确展示，不再重复显示修改条目。重复声明保持首次顺序并采用最新说明。
5. 文件卡与最终回答解耦；工具已经成功而回答中断时，文件事实仍保留。

这些规则不从模型回答、命令字符串或终端输出猜测路径。历史没有可靠证据时不补造产物。

## 资源身份、能力和宿主操作

统一 `FileResourceRef` 包含来源类型、host、tab、源调用和路径。相同路径来自不同主机、会话或调用时不会被当成同一资源。来源证明与读取能力分离，调用 ID 不是任意路径通行证。

统一入口为：

```ts
openResource(ref, { view: "preview" | "source" | "browser" })
performResourceAction(ref, action)
resolveFileResourcePath(ref)
```

UI 先查询能力快照，只显示可用操作；宿主仍在每次执行时重新校验。浏览器打开使用可取消的宿主就绪订阅，不再逐动画帧轮询。新打开请求或会话切换使旧 intent 失效，并撤销已生成但未消费的预览 URL。

远程 `present` 继续要求 `present-files-v1` 和历史中的可信声明。远程文件修改通过 tab、host、client、generation、当前会话和成功内置工具证据验证，再将相对路径限制在远程工作区内。远程路径不会传给本机系统打开接口。

## 文档预览与生命周期

当前 Workspace dock 继续承担 Markdown、HTML、源码、CSV、图片、PDF 和音视频预览。资源标签保留来源范围、视图模式、换行和阅读位置；分页版本改变时停止拼接并要求刷新。

HTML 沿用现有媒体 URL 和独立预览源：iframe 为 `sandbox="allow-scripts"`，没有同源、Node、应用 bridge 或顶层导航权限。依赖预算仍是总量 32 MiB、最多 64 项、单项 4 MiB；当前 CSP 的 HTTPS 范围保持不变。iframe 隔离不等于网络隔离。HTML 必须拿到完整文档才执行，截断预览不会运行。

全文、源码分页和依赖准备共享每会话最多 4 个应用请求的调度。关闭内容取消无消费者请求；无法取消的后端结果会因 generation 失效而被丢弃。切换会话释放节点订阅、内容任务、抽屉、媒体实例、资源 URL 和待消费 intent。

## 滚动和导航

聊天继续使用自然文档流和单一 `ChatScrollController`。所有程序化 `scrollTop` 写入经单一 writer；用户向上输入立即退出跟随。历史 prepend、过程折叠、图片加载、文件 dock 引起的宽度变化使用稳定节点和视口偏移恢复阅读位置。

轮次导航只列已加载轮次并沿用 1.38.7 的颜色。打开工具抽屉覆盖正文，不改变正文宽度。打开文件 dock 会改变工作台分栏，控制器在该用户操作后恢复阅读锚点。

## Provider 与兼容边界

当前 `present` schema、描述、工具排序、provider 请求序列化、system prompt 和历史格式保持不变。新增的远程宿主方法是可选的客户端能力；旧端缺少能力时只显示已有能力，不把远程路径替换为本机路径。

旧历史继续读取。派生调用、轮次和文件索引不落盘。未知内容由局部兜底 renderer 处理，不修改原始历史。

## 2026-09-13 验收结果

| 宿主 | 场景 | 输入 P95 | 切换 P95 | 最大长任务 | 锚点漂移 |
|---|---:|---:|---:|---:|---:|
| Chromium 153 | 240 轮 | 34.4 ms | 37.6 ms | 63 ms | 0 px |
| Chromium 153 | 1,000 轮 | 105.7 ms | 37.6 ms | 243 ms | 0 px |
| WebKit 26.6 | 240 轮 | 57 ms | 54 ms | 不可测 | 0 px |
| WebKit 26.6 | 1,000 轮 | 63 ms | 54 ms | 不可测 | 0 px |
| Electron 44.2 | 240 轮 | 30.1 ms | 24.6 ms | 72 ms | 0 px |
| Electron 44.2 | 1,000 轮 | 43.2 ms | 24.6 ms | 272 ms | 0 px |

Chromium 强制回收后的 20 次会话切换堆增长为 223,692 字节，低于 20 MiB 门槛。三个宿主的历史 prepend 锚点变化均为 0.09375 px。WebKit 没有 Long Tasks API，因此该列保持“不可测”。

通过的检查包括前端 typecheck、test typecheck、lint、build、bundle 预算、单滚动写入、transcript、stream、composer、workspace、remote、app lifecycle、MCP、性能投影、Chromium/WebKit/Electron 完整聊天回放和 Electron 42 个几何场景；根 Go 包、Desktop Go 模块和相关 race 检查通过。

原始结果和关键截图：

- [Chromium 原始数据](evidence/chat-content-host-2026-09-13/chromium/chromium-results.json)
- [WebKit 原始数据](evidence/chat-content-host-2026-09-13/webkit/webkit-results.json)
- [Electron 原始数据](evidence/chat-content-host-2026-09-13/electron/electron-results.json)
- [Chromium 聊天](evidence/chat-content-host-2026-09-13/chromium/chromium-chat.png)
- [Chromium 文件卡](evidence/chat-content-host-2026-09-13/chromium/chromium-presented-files.png)
- [Chromium 折叠过程](evidence/chat-content-host-2026-09-13/chromium/chromium-weather-collapsed.png)
- [Chromium 展开过程](evidence/chat-content-host-2026-09-13/chromium/chromium-weather-expanded.png)
- [Electron 聊天布局](evidence/chat-content-host-2026-09-13/electron/electron-chat.png)
- [完整证据索引](evidence/chat-content-host-2026-09-13/README.md)

## 未验证项

- 未连接实际远程服务器执行网络端到端回放；远程来源隔离和降级由协议、宿主与 UI 测试覆盖。
- 未在 Windows 和 Linux 原生桌面运行系统打开、编码器和输入法验证。
- WebKit 的长任务指标不可测；没有将缺失值记录为零。
- 本次 Electron 回放使用真实 Electron 宿主和生产组件 fixture，没有启动签名后的发布包，也没有在隔离图形环境运行小时级 macOS 原生输入法浸泡。

这些项目不会由浏览器结果替代，也不阻塞当前 macOS 实现和自动化回归结论。
