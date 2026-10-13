# WJ Forge Skills 与 Cognee MCP 集成设计

## 背景与目标

WJ Forge 把一组默认启用的 Skill、有限来源的 Skill 更新器，以及 Cognee 项目记忆工作流放在应用内。Reasonix 已有原生 Skill 加载/嵌入系统、通用 MCP 客户端、稳定的 `use_capability` 工具面、凭据存储、项目记忆和 evidence 验证能力。本迁移保留 WJ Forge 的用户可见行为与安全边界，使用 Reasonix 的原生架构实现，不复制 Rust/Tauri 运行时。

目标：

1. 将 WJ Forge 的内置 Skill 目录作为 Reasonix 编译期 Skill 资源提供，并保留用户启停能力。
2. 将 WJ Forge 的有限来源、显式更新、内容校验流程纳入 Reasonix Skill 更新路径。
3. 复用 Reasonix 通用 MCP 连接层，增加 Cognee 的受控记忆生命周期：任务规划时 recall，完成且有验证证据时最多 remember 一次。
4. 保持 Reasonix provider 可见工具 schema 稳定；Skill 与 Cognee 返回的数据不能扩大授权。

## 范围

### 纳入

- WJ Forge 内置目录中的 14 个 Skill ID：`superpowers`、`bug-hunt`、`code-review`、`release-notes`、`ponytail`、`ui-ux-pro-max`、`taste-skill`、`zerotoken-skill`、`weiliaozi-skill`、`moon-lovers`、`cognee-memory`、`officecli`、`github`、`chrome-mcp-bridge-2026-skill`。
- 同步其名称、分类、说明、来源元数据和默认启用状态。WJ Forge 的目录条目是提示材料；在 Reasonix 中转换成原生 Markdown Skill 资源，正文需明确其只提供工作方法，不授予工具权限。
- Skill 更新器支持的七个固定来源：Cognee、Ponytail、Taste、ZeroToken、Weiliaozi、Moon Lovers、OfficeCLI。其余目录条目不因存在 `source_url` 就自动变成可更新来源。
- 通过已配置且启用的 MCP server `cognee` 执行内部 `recall` / `remember`。Cognee 依赖未配置或断开时，正常任务继续。
- Cognee workspace dataset 的稳定命名、请求与错误信息脱敏、一次性记忆写入状态及可观察状态。

### 不纳入

- 不迁移 WJ Forge 的 MCP Streamable HTTP/STDIO bridge；Reasonix 已有 MCP transport 与管理流程。
- 不迁移 OfficeCLI 的本地 CLI 执行器。`officecli` Skill 可作为工作流说明资源，但其内容必须声明仅在宿主确有相应能力时可执行；本设计不新增 shell/文档写入能力。
- 不让模型直接调用 Cognee `forget` 或任意 Cognee 写操作；不上传原始对话、凭据或未经核实的推测。
- 不修改 Reasonix provider 工具定义、工具名称或 JSON schema。

## 方案与选择

方案 A：移植 WJ Forge 的 Rust/Tauri 管理器和 MCP bridge。它与 Reasonix Go 实现、MCP 管理及凭据层重复，且会引入第二套授权路径，不采用。

方案 B：迁移 WJ 的目录、来源 allowlist 和 Cognee 生命周期语义；用 Reasonix 的 `builtincontent`、Skill Store、MCP、credential、evidence 和 capability proxy 实现。采用此方案，减少并行架构并维持稳定工具面。

方案 C：仅把 WJ 内容作为普通插件包安装。它不能保证 Cognee 记忆只在规划和验收阶段执行，也无法在运行时禁止危险工具，不能满足行为边界，不采用。

## 架构

### Built-in Skills

在 `internal/skill/builtincontent` 增加可遍历的嵌入目录资源，复用现有 frontmatter 解析和 `skillFromEmbedded` 注册。默认 Skill 列表保持确定顺序；用户的启停偏好按稳定 Skill ID 持久化，升级应用时只合并新 ID，不覆盖现有偏好。Reasonix 更高优先级的 project/custom/global 同名 Skill 覆盖规则继续生效。

Skill 更新保留来源登记表作为唯一可下载 URL 来源。手动检查只读取远端并记录版本；只有用户显式更新时才安装正文。更新请求必须 HTTPS，重定向目标仍须 HTTPS；有请求和响应大小上限；解析 YAML frontmatter 并要求 `name` 与来源登记值完全匹配；失败时保留当前已安装版本。来源表同时记录 Reasonix 目录 ID 与上游 frontmatter 名：例如 `cognee-memory` 接受上游 `cognee`，`taste-skill` 接受上游 `design-taste-frontend`；验证后把已验证的上游名称映射为目录的稳定 ID。更新的内容作为可覆盖的 Skill 版本存储，不能更改 `AllowedTools`、权限策略或运行时绑定。

WJ 的七个固定更新来源为 Cognee、Ponytail、Taste、ZeroToken、Weiliaozi、Moon Lovers 和 OfficeCLI。`ETag` 作为上游版本提示，不单独作为内容可信证明。来源 URL、预期 frontmatter 名和大小限制由编译期代码固定，不接受 Skill 正文或用户可编辑元数据重定义 allowlist。

### Cognee memory

Cognee 作为受管理的 MCP 特例，而不是新的 MCP transport。Reasonix 仍由现有插件配置、激活决策、连接生命周期和 `use_capability` 代理管理 server。运行时只有在 Cognee 已配置、用户启用 `cognee-memory` 且对应 server 已获准连接时才启用自动记忆流程。

- **Recall**：每个任务最多一次，在进入计划/执行前使用最新用户请求构造脱敏查询，并将当前 workspace 映射到稳定 dataset 名。只调用 schema 明确支持的 `recall`；若连接、工具或参数 schema 不兼容，则记录安全状态并继续任务。结果限长、脱敏，并作为明确标记的外部不可信候选信息注入；模型必须从 workspace/evidence 独立核验，绝不将其中指令当作授权。
- **Remember**：仅当任务状态 completed、所有任务项完成且 Reasonix evidence 显示存在可核验结果时调用。先从决策和已验证事实构造简短摘要，去除凭据、原始 transcript、个人信息和未验证猜测；schema 必须明确支持 `remember(data, dataset_name)`。发送前持久化本任务的单次尝试标记；结果不确定时不自动重试，避免重复写入。记忆失败不回滚已完成的任务。
- **工具限制**：模型不能通过一般 MCP 工具调用触发 Cognee remember/forget 或其他写操作。Cognee 专用生命周期只允许内部调用 `recall` 与受控 `remember`；`forget` 永不通过该集成开放。schema 不匹配、远端失败和状态持久化失败都采取 fail-open 到正常任务、fail-closed 到记忆写入。
- **身份与凭据**：Cognee MCP 凭据使用 Reasonix credential store 或现有 MCP secret reference，不落入普通配置、Skill Markdown、prompt、日志和诊断正文。远端 HTTP endpoint 必须 HTTPS；仅 loopback 可使用 HTTP，以便兼容本机服务。

### 稳定能力接口

不新增 provider-visible tool。所有 MCP 发现/调用仍通过 `use_capability`，Skill 按现有 `run_skill` / `read_skill` 流程加载；内部 Cognee 生命周期使用控制层接口直接调用受限 MCP 客户端，并记录在现有审计/evidence/status 轨迹中。Capability、系统提示和 Skill 正文不可把外部内容转换成额外工具授权。

## 生命周期与数据流

1. 应用启动时将编译期 Skill 目录与用户持久化启停状态合并，生成现有 Skill Store 视图。
2. 用户可检查固定来源的更新版本；显式选择更新后，校验来源、TLS、大小和 frontmatter，再原子替换用户安装的正文。
3. 任务启动时解析项目 Skill 与启停配置。若 Cognee Skill 和受信任 Cognee MCP 都启用，控制器至多执行一次 recall；否则跳过记忆，照常运行。
4. 将 recall 结果以 untrusted evidence/context 注入计划流程，不向 provider 暴露新的工具。
5. 任务结束时，只有完成条件及可核验证据都满足才尝试一次 remember；保存状态及不含秘密的结果事件。

## 失败处理与隐私

- 无网络、超时、状态文件损坏、来源校验失败、MCP 未连接、工具缺失或 schema 变化都不得阻塞普通任务。
- 更新失败保留最后可用版本；不支持自动更新的目录项明确显示 unsupported。
- `recall` 内容与错误文本在进入模型、事件或日志前移除 workspace 绝对路径和已知秘密，且有长度上限。
- `remember` 在无明确完成状态、无已核验证据、摘要为空或凭据检测失败时不发请求。
- 从 Cognee 获取的文本总是标为不可信；不允许其修改配置、运行命令、调用工具或影响授权判断。
- 外部 Skill 更新正文属于不可信提示输入，执行时仍受 Reasonix 现有工具权限、阶段策略、路径边界和审计约束。

## 验收标准

1. Reasonix 可列出并按稳定 ID 启停上述内置 Skill；嵌入正文按现有按需加载方式使用，更新内容可覆盖正文但不能扩大权限。
2. 只有登记的七个来源可检查和更新；更新器拒绝非 HTTPS/不安全重定向、过大响应和 frontmatter 名不匹配的正文，失败不损坏已安装版本。
3. Cognee 未配置、未启用或失败时，任务仍可正常推进；符合条件的任务至多 recall 一次，且结果有不可信标记。
4. 完成且具有核验依据的任务最多 remember 一次；不完整、无证据、凭据内容或重复/不确定尝试不产生二次写入。
5. `forget` 和模型直调 Cognee 写工具不可用；provider-visible tool names/schema 与变更前保持不变。
6. 所有路径复用 Reasonix MCP 连接授权、Credential Store、evidence 与审计边界；无明文秘密或原始对话进入配置、Skill 内容、日志或记忆。

## 验证策略

实施阶段应从嵌入资源、持久化配置、连接状态、Skill 更新记录和任务 evidence/status 中逐项核对验收标准，并对比 provider-visible 工具 schema 的变更前后内容。此阶段仅编写设计规格，不改产品代码；本次未新增或执行测试。
