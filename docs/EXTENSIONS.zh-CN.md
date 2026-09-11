# Reasonix 扩展

扩展让插件包在运行时改变 Reasonix 的行为——改写输入、拦截工具调用、
替换系统提示词、提供流式模型 Provider、发布结构化 UI，以及分发
prompts 和主题——全部基于稳定、带版本号的契约。

插件能力分两类：

- **声明式**（任意插件包）：skills、agents、commands、prompts、hooks、
  MCP servers 和主题。它们是文件与配置，按宿主正常权限运行。
- **代码型 Runtime**（Manifest v2 的 `runtime` 块）：通过 Extension
  Protocol 驱动的 Sidecar 进程。代码型扩展是**完全信任（full trust）**
  的——安装前请务必阅读下文安全章节。

## 安装与管理

扩展的安装方式与普通插件包完全一致：

```bash
reasonix plugin install git:github.com/owner/extension --dry-run   # 预览
reasonix plugin install git:github.com/owner/extension --yes       # 安装
reasonix plugin show <name>                                        # 详情
reasonix plugin doctor <name>                                      # 校验
```

带有 `runtime` 块的插件，其预览与 `show` 输出会包含 **FULL TRUST**
区块：Runtime 命令、拦截的事件、持有的替换槽，以及 Provider/UI 能力。
安装、更新、替换或 `--link` 即代表授权——没有二次确认，`--link` 在内容
变化后自动保持信任。请只安装你完全信任的运行时。

## 扩展能做什么

- **拦截器（Interceptors）**——观察并裁决 17 个 hook 点（输入、工具
  调用、权限判定、Provider 请求/响应、压缩、会话生命周期、前端事件）。
  拦截器可以 `continue`、`block`（给出用户可见原因）或 `replace`
  （替换载荷）；宿主会对每个替换重新校验。
- **替换策略**——单 owner 槽位（`system_prompt`、`context`、
  `provider_request`、`provider_response`、`compaction`、
  `session_policy`、`permission`、`frontend_events`、`tool:<name>`、
  `provider:<ref>`）。同一槽位在所有已安装插件中只能有一个 owner，
  争用会令运行时构建失败并列出来源。
- **流式 Provider**——新模型以 `plugin/<plugin>/<provider>/<model>`
  出现在模型选择器中，流式语义（text/reasoning/工具调用/usage）与
  内置 Provider 一致。该 ref 可用于任何内置 ref 可用之处：
  `default_model`、`--model`、CLI/Desktop/ACP 模型选择器以及会话中的
  模型切换——包括首次启动。
- **结构化 UI**——status、card、form、notification 在 CLI transcript、
  Desktop 与 ACP 客户端中原生渲染（不支持时退化为文本），action 同时
  出现在 `/<plugin>:<action>` 斜杠菜单、Desktop 命令面板和 ACP 可发现
  命令中。
- **Prompts 与主题**——`/<plugin>:<name>` 提示词模板，以及 Desktop
  设置中的只读插件主题（`plugin:<plugin>:<theme>`）。

## 运行时重载

已安装扩展发生变化（安装、更新、启用/禁用、`--link` 内容变化）绝不会
修改正在运行的回合。所有交互前端都提供失败原子的重载入口——CLI
`/reload`、Desktop `/reload`（输入框斜杠菜单）或「重载运行时」（命令面板）、
Serve `/reload`、ACP
vendor method `_reasonix.io/session/reloadExtensions`：

1. 回合或后台任务运行中，CLI/Desktop/ACP 只排队一次；Serve 会拒绝本次
   请求，由浏览器在空闲后重试。
2. 空闲后启动新 Sidecar 并构建新的运行时快照。
3. 完整成功后原子交换，并迁移 session path、transcript、授权记录和
   goal/recovery 状态。
4. 新构建失败时，旧运行时不受影响继续可用。
5. 交换完成后才关闭旧 Sidecar。

每个回合自始至终（含工具批次与压缩）固定使用同一个运行时
generation——扩展变更从下一个回合生效；no-op 重载后 Provider 提示词
缓存前缀字节不变。

## 性能与提示词缓存

未安装代码型 Runtime 时，Agent 仍走原有 nil-dispatcher 路径：不会启动
Sidecar，也不会发生 JSON 编码、RPC 或事件排队。安装 Runtime 后，Reasonix
在同一个 generation 的 30 秒总启动预算内最多并行初始化 4 个 Sidecar；
卡住的可选 Runtime 不会再按已安装包数量成倍拉长启动或 reload。未能在
预算内启动的包按其 `runtime.required` 设置降级或令构建失败。启用后的
同步拦截器会串行进入相应热路径，因此 RPC 与处理耗时会累加；输入、工具、权限和
Provider 拦截器应保持轻量且结果确定。观察事件通过有界非阻塞队列投递，
背压时告警并丢弃，不会卡住当前回合。

纯观察扩展不会改变 Provider 可见缓存前缀。稳定的系统提示词或工具替换
会在安装/重载后产生一次预期的冷前缀，之后仍可持续命中缓存；若策略把
时间戳、随机值、session ID 或其他逐回合动态数据写入系统提示词、工具
Schema、上下文前缀或 Provider 请求，则可能破坏缓存复用。动态数据应尽量
留在当前回合尾部。维护者可用以下命令测量宿主开销：

```bash
go test ./internal/extension/... -run '^$' -bench 'Extension|Dispatch' -benchmem
```

## 开发扩展

建议从完整的
[`starterextension`](../sdk/go/examples/starterextension/README.zh-CN.md)
开始。它把 Manifest、Sidecar 源码、跨平台构建命令、链接安装和第一个可观察
拦截效果放在同一目录。标准开发流程是：

1. 在 `reasonix-plugin.json` 中加入
   `apiVersion: "reasonix.io/plugin/v2"`，声明 `contributes` 与
   （可选的）`runtime`——见
   [插件包文档](./PLUGIN_PACKAGES.zh-CN.md#manifest-v2扩展)。
2. 实现 Sidecar。[Go SDK](../sdk/go/README.md)（仅依赖标准库）已经处理传输、
   握手、序号、content ref 与关闭；语言无关的参考见
   [线协议](./EXTENSION_PROTOCOL.zh-CN.md)和
   [生成方法索引](./EXTENSION_PROTOCOL.generated.md)。
3. 构建 Runtime 二进制，先用
   `reasonix plugin install /path/to/plugin --dry-run` 检查信任与能力，再用
   `--link --yes` 安装。
4. 用 `reasonix plugin doctor <name>` 校验，在空闲时运行 `/reload`，然后验证
   插件贡献的拦截器、Provider、UI action 或资源。

SDK 使用不可变的 `sdk/go/vX.Y.Z` 标签发布，首个公开版本为
`sdk/go/v1.0.0`。该标签存在之前，请从源码 checkout 使用 starter，不要依赖
未版本化的 module API。

## 兼容性

- 原生 `reasonix-plugin.json` 必须声明精确版本
  `reasonix.io/plugin/v2`。扩展 Manifest v1 从未公开发布，因此不提供
  v1 双读或自动迁移路径。
- 旧版本 Reasonix 会忽略扩展专有状态：会话级
  `<session>.extensions.json` sidecar 文件、`plugin/...` 模型 ref
  （仅报告模型不可用），以及 `extension_surface`/`extension_status`
  事件类型（旧前端丢弃未知类型；未声明 `reasonix.extensionSurface`
  的 ACP 客户端收到文本 fallback）。
- `plugin-packages.json` 保持现有 schema；已启用的已安装 Runtime 即
  为信任记录。

## 安全模型

代码型扩展运行在 Reasonix Sandbox 之外，继承未过滤的完整环境：可以
读取完整会话与环境、绕过权限与工作区限制、直接操作本机；它在
`permission.decision` 上的 "allow" 可覆盖宿主 deny。作为约束，宿主
保证：

- 只有通过插件安装流程的插件才能启动 Runtime——项目配置永远无法
  声明代码型 Sidecar；
- 握手时拒绝任何超出 Manifest 声明的能力；
- 所有替换都按点位 DTO 与 Schema 重新校验；
- Sidecar 的诊断输出、结构化 UI、拦截器原因和 Provider 错误在进入 UI、
  日志或错误界面前由宿主进行凭据脱敏；普通 Provider/模型内容作为产品
  数据保持原样；
- Sidecar 崩溃只令其自身操作明确失败——Reasonix 绝不静默回退到
  其他模型或策略。
