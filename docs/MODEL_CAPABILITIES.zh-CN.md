# 模型能力元数据

Reasonix 通过 Provider Adapter 按具体模型解析输入能力。Adapter 遵循
`deepseek-harness` 的模型契约，为精确模型返回 `inputModalities`：

- `text` 表示支持文本输入；
- `image` 表示支持原生图片输入；
- `text + image` 会在不配置 `VisionModels` 的情况下自动启用多模态请求。

OpenAI-compatible `/models` 响应优先使用标准字段 `input_modalities`，同时
兼容 `modalities.input`、`capabilities.input_modalities`、
`capabilities.vision`、`supports_vision` 和 `vision`。缺失、无效或冲突声明保持
“图片能力未识别”（内部为 `nil`，Desktop 返回 `[]`）；有效的文本声明表示
不支持，包含图片的声明表示支持。标准字段优先于兼容别名，即使标准字段
无效也不会由别名偷偷开启。重复 ID 合并与顺序无关：未知项不覆盖有效声明，
相互矛盾的有效声明最终保持未知。不会根据模型名称猜测视觉能力。

动态元数据保存在 Reasonix 缓存目录下独立的
`model-capabilities-v2.json` 文件中，不会写入 `config.toml`。已有的
`vision` 和 `vision_models` 配置仍可读取，并优先于动态元数据。V2 不读取、
迁移或修改 V1，因为旧缓存无法区分缺字段与明确否定。只依赖 V1 正面缓存
的自定义模型需要刷新一次模型列表，也可以手动开启。缓存仍保留 24 小时
TTL；请求失败不覆盖成功结果，成功返回 ID-only 时会记录未知事实。
路由与凭据变化隔离缓存，晚到的旧请求不能覆盖较新的成功发现。

内置 Adapter 还会为所有未被修改的精选 Provider 预设生成本地校验目录，
覆盖官方 OpenCode Go 路由、DeepSeek vision SKU、ModelScope Qwen3.5 SKU 及
其他预设模型列表，因此不依赖模型列表请求即可工作。自定义 Endpoint、已
编辑的预设或本地目录之外的模型，在没有其他有效来源时保持未知。

## 中转站模型使用指南

1. 在“设置 → 模型”新增或编辑接入，获取模型列表。
2. 勾选需要的模型。若显示“图片能力未识别”，向服务商确认支持后，将
   “图片输入”选为“开启”。
3. 点击保存，等待运行时重建成功，再发送图片。刷新列表和重启会保留选择。

接入编辑器和刷新模型列表入口均提供“自动 / 开启 / 关闭”。开启表示用户
声明此接入支持图片，不代表客户端已经实测，也不会触发付费能力探测。关闭
会禁止原生图片输入，包括本地目录已知的视觉型号。自动只清除当前模型的
`vision` 覆盖，保留上下文、输出与 reasoning 设置；若沿用旧配置，会显示
“自动 · 沿用旧配置”。

```toml
[providers.model_overrides.example-model]
vision = true # false 为关闭；删除此字段恢复自动
```

最终优先级：官方协议硬限制 → 当前模型显式覆盖 → 原自动链路（精选预设、
旧配置、本地精确目录、有效在线缓存）→ 未知。官方 DeepSeek 文本型号不能
被手动开启，其视觉型号可以显式关闭。能力覆盖不会丢失本地目录的上下文、
输出上限、协议与 reasoning 元数据。界面回传的能力信息不作为发现事实保存。

当前空闲会话保存后重建；其他已打开会话在下一回合前检查并重建。正在执行
的请求保持冻结的能力和请求体。若保存或重建失败、延期，需要完成重建后才
生效；输入框读取对应 Controller 的能力快照。不会自动重发失败请求或历史
图片，发送消息也不会临时请求 `/models`。

系统提示词、工具 Schema 和纯文本请求序列化保持稳定。能力切换可能改变
包含历史图片的上下文投影，因此不能保证这些会话在重建前后缓存命中不变。

更完整的 Provider/模型目录来源于 MIT 许可的
`github.com/sky-valley/pi/ai` Go 版 Pi。Reasonix 只使用其嵌入的模型数据
（`GetModels`、`Model.Input` 及相关字段），不引入它的 Agent 或 Provider
运行时。依赖版本固定在 `go.mod` 中，目录更新需要按数据和许可证变更审查。

Dependabot 每周为 `sky-valley/pi` 单独创建升级 PR，不与无关的 Go 依赖混合。
`internal/provider/opencode_go_test.go` 中的 catalog contract tests 会在关键
模型能力发生漂移时失败，因此升级前必须审查 Provider、API 和 Endpoint 差异。

文本模型和未知模型继续使用现有的 `Agent.VisionModel`、OCR、MCP vision
fallback。原始图片不会发送给这些模型。
