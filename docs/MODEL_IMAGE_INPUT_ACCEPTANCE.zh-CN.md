# 模型图片输入改造验收记录

实施基线：`main-v2` / v1.37.0，`2c65a55abbca638c58e1bd8c5a21602e455f99a0`；
交付前已重放到最新 `main-v2`，保留后续合入的 Desktop 修复。
本次仅交付代码、测试和文档，不包含合并或版本发布。

## 发现项、修复与验证

| 发现项 | 修复 | 验证结果 |
| --- | --- | --- |
| `/models` 缺字段被补成文本，无法区分未知和否定 | 保留 `nil`；无效字段保持未知；重复 ID 冲突保持未知且与顺序无关 | `fetch_models_test.go` 覆盖标准字段优先级、畸形字段、布尔 null 与重复排列 |
| 设置页、目录与运行时可能遗漏当前模型的覆盖 | 共享解析器直接读取逐模型覆盖，分别返回自动与最终结果，保留目录元数据 | `model_capabilities_v2_test.go` 验证不修改输入、大小写匹配、其他模型隔离、目录事实保留 |
| 官方视觉型号的特例绕过显式关闭 | 三个 Provider 构造器尊重已解析元数据；官方文本限制仍优先 | Chat、Messages、Responses 的 `image_test.go` 验证关闭后的请求体没有原图 |
| 独立完整请求地址可能漏掉官方限制 | 同时检查实际请求地址，保持其他协议行为不变 | 三种协议的 `TestOfficialRequestURLImageHardLimit` 捕获序列化结果，确认图片不能绕过限制 |
| 没有可操作的逐模型入口 | 两个选择器复用自动／开启／关闭控件；支持禁用、取消、重新勾选、旧配置来源提示 | `provider-image-input.test.tsx` 与真实浏览器检查通过；简中、繁中、英文文案同步 |
| 多模型卡片被压成窄列，能力说明和上下文层级混乱 | 两个入口统一为轻边界的一行一模型设置列表；策略复用全项目 `set-seg` 控件并保留原生 radio 语义；窄空间自动分层 | 浏览器验证四模型布局、鼠标选择与方向键切换；控制台无 warning/error |
| 自动模式错误沿用先前手动开启值 | 自动降级只使用真实旧配置，不使用覆盖投影 | 编辑器回归测试与浏览器实际切换确认显示“图片能力未识别” |
| 刷新可能清理用户模型或覆盖其他参数 | 候选列表合并保留已配置模型；覆盖合并只修改 vision | 编辑器保存测试保留上下文、输出和 reasoning；HTTP 集成测试验证刷新与重新加载后的持久化 |
| v1 缓存无法还原未知语义 | 使用独立 v2 文件，不读取、迁移或修改 v1 | v2 测试验证未知往返、版本拒绝、损坏处理、0600 权限及磁盘合并校验 |
| 路由字段遗漏、发现请求乱序 | 补齐 NoProxy、ChatURL、RequestURL 指纹；提交前检查凭据和配置身份；按请求开始时间合并成功结果 | 通道控制的 Desktop 测试覆盖路由改变、凭据改变、乱序完成、失败不抹除成功缓存；race 通过 |
| 前端编辑器关闭后旧结果可能回填 | 请求代次与编辑器身份共同校验；保存密钥后刷新使用新指纹 | 可控 Promise 验证旧编辑器结果不能进入新草稿 |
| 保存后界面与 Controller 能力不一致 | Controller 持有冻结能力；当前会话保存重建；其他标签在下一回合前复用现有重建入口 | `TestImageInputSaveRebuildsActiveAndNextTurnRefreshesOtherTab` 验证控制器替换和 `MetaForTab` 的实际结果 |
| 图片能力切换可能意外改变文本请求前缀 | 不修改系统提示词或工具 Schema；只在重建边界切换能力 | 三种协议捕获真实 HTTP 请求，比较开启前后的无图片请求完整结构 |

## 核心路径

`desktop/model_image_input_test.go` 中的
`TestIDOnlyRelayImageInputSettingsToWire` 对 OpenAI Chat、Anthropic Messages、Responses
各运行一次以下流程：

1. 本地 HTTP 中转站返回只有 ID 的模型列表；能力 DTO 为 unknown，数组为 `[]`。
2. 保存接入；未知模式请求不包含原生图片。
3. 经 `SaveProvider` 保存逐模型 `vision=true`，通过实际 Provider 工厂发送图片。
4. 模拟服务端捕获正确的 `image_url`、`image` 或 `input_image` 内容块。
5. 新建 App、重新加载配置并再次刷新模型列表，覆盖仍为开启，自动结果仍为未知。
6. 保存关闭、恢复自动，确认请求不再包含原图，其他模型参数保留。

该测试还传入伪造的前端 `modelCapabilities`，确认其不会成为配置或缓存中的自动发现事实。
所有请求使用本地模拟服务，不依赖用户密钥，不执行付费能力探测。

## 检查命令

最终结果：根 Go 模块与 Desktop 独立 Go 模块的完整测试全部通过；下列两组 race 测试全部通过；前端完整测试及生产构建通过。`git diff --check` 通过。

根 Go 模块：

```sh
go test ./...
go test -race ./internal/config ./internal/control ./internal/boot ./internal/provider/openai ./internal/provider/anthropic ./internal/provider/responses -run 'Capability|Vision|Image|FetchModel|ProviderResolver'
```

Desktop 独立 Go 模块：

```sh
go test ./...
go test -race . -run 'TestImageInputSaveRebuilds|TestIDOnlyRelay|TestDiscovery|TestSaveProvider|TestProviderModel|Test.*TabMeta'
```

前端：

```sh
pnpm test:all
pnpm build
```

生产构建包含类型检查、Hook lint、WAAPI、滚动写入约束、CSS 语法、z-index、主题 Token 和 Bundle 预算检查。
新增本地化提示和共享匹配逻辑带来小幅包体增长；预算按实际测量小幅调整，保留独立 chunk 和 CSS 门槛。
前端完整检查通过，包括主测试运行器的 255 个套件、Remote 测试与长历史性能契约。

## 浏览器与验证边界

真实浏览器访问 Vite 的 mock bridge 页面，检查新增／编辑入口及刷新后的候选列表：
选择、取消、保存、再次打开、刷新保留选择、键盘操作，以及 780 × 720 窄窗口布局。
手动开启后切回自动正确显示未知；未获取元数据的官方文本模型也会禁用开启选项。
检查时浏览器未记录 warning/error。

浏览器检查使用模拟桥接，不等同于打包后的 Wails 原生窗口端到端测试。
Desktop 保存后的真实配置、Controller 重建和能力元数据由 Go 集成测试验证；
模拟重启指新建 App 与重新加载持久配置，没有启动第二个原生应用进程。

## 兼容性与缓存影响

- 继续使用原有 TOML `model_overrides.<model>.vision`；自动删除该字段，只在整个覆盖为空时清理条目。
- 旧 `vision`、`vision_models` 保留；旧 ID 列表接口保留。
- 仅依赖 v1 正面在线缓存的中转站模型，升级后需重新获取列表或手动开启；官方本地目录仍离线可用。
- 失败请求不自动重发；发送阶段不请求模型列表。进行中的回合不切换快照。
- 无图片请求保持稳定；包含历史图片的上下文投影可能改变，因此不保证此类会话缓存命中完全不变。

使用说明：[中文](MODEL_CAPABILITIES.zh-CN.md) / [English](MODEL_CAPABILITIES.md)。
