# Reasonix 维护接手与学习指南

这份指南的目标不是让你一次读完全部代码，而是用可验证的纵向路径，逐步获得安全修改项目的
能力。架构全景见 [`ARCHITECTURE.zh-CN.md`](ARCHITECTURE.zh-CN.md)，应用级恢复契约见
[`RECOVERY.zh-CN.md`](RECOVERY.zh-CN.md)。

## 1. 接手目标

完成下面四个里程碑，才算真正建立维护能力：

1. 能从入口追踪一次用户回合到 Provider、Tool、事件和持久化。
2. 能判断一个修改应落在 boot、control、agent、domain package 还是 frontend。
3. 能为修改选择正确的验证矩阵，而不是只跑一个局部测试。
4. 能识别 cache、权限、会话身份、应用恢复和跨平台构建这五类非显然风险。

## 2. 开发环境先对齐

### 2.1 根 Go 模块

仓库根 [`go.mod`](../go.mod) 是版本事实来源；当前同时声明 `go 1.25.0` 和
`toolchain go1.26.5`。不要只按 README 中的宽泛版本安装。先检查：

```bash
go version
go env GOTOOLCHAIN GOOS GOARCH
git status --short --branch
```

使用能识别 `toolchain` 指令的现代 Go，并让它采用仓库声明的工具链。若 `go` 在解析
`go.mod` 时就报 `unknown directive: toolchain`，问题在本机 Go 过旧，尚未进入项目编译阶段。

### 2.2 桌面端

桌面端需要目标 OS 的原生环境：

- Go：同 `desktop/go.mod`；
- Node 24、pnpm 10：与 CI 对齐；
- Wails CLI 2.12.0：与 `desktop/go.mod`/CI 对齐；
- Windows WebView2，macOS WebKit，Linux GTK/WebKitGTK 开发库。

Wails/CGO 桌面程序不是根 CLI 的普通交叉编译目标。Windows UI 和打包应在 Windows 原生
checkout 中验证；Linux/WSL 的根模块通过只能证明 CLI/kernel 那一部分。

### 2.3 其他表面

- `site/` 使用 npm 和 Node 24。
- `workers/*` 使用各自 lockfile、TypeScript 和 Wrangler。
- 只有维护对应站点、Worker 或发布通道时才需要安装这些依赖。

## 3. 永远先隔离开发状态

开发版默认不应读取或污染日常使用的 Reasonix 状态：

```bash
export REASONIX_HOME=/tmp/reasonix-dev
go run ./cmd/reasonix --help
```

随后所有配置、credentials、sessions、cache、skills、commands、hooks 和桌面 tab state 都会
留在这个目录。Windows PowerShell 使用：

```powershell
$env:REASONIX_HOME = "$env:TEMP\reasonix-dev"
```

测试恢复、迁移或删除时，为每个场景使用新的临时 home，避免“旧状态恰好让测试通过”。

## 4. 第一遍阅读：只走主链

按以下顺序阅读，不要一开始钻进几千行的 UI 或配置字段：

1. [`README.zh-CN.md`](../README.zh-CN.md)：产品边界和用户概念。
2. [`REASONIX.md`](../REASONIX.md)：维护不变量，尤其是单一 Controller 和 cache-first。
3. [`cmd/reasonix/main.go`](../cmd/reasonix/main.go)：空导入和真正入口。
4. [`internal/cli/cli.go`](../internal/cli/cli.go)：命令如何分流。
5. [`internal/boot/boot.go`](../internal/boot/boot.go)：系统如何装配。
6. [`internal/control/port.go`](../internal/control/port.go)：前端能驱动什么。
7. [`internal/control/turn_orchestrator.go`](../internal/control/turn_orchestrator.go)：一回合的主流程。
8. [`internal/agent/agent.go`](../internal/agent/agent.go)：模型/工具循环。
9. [`internal/provider/provider.go`](../internal/provider/provider.go) 和
   [`internal/tool/tool.go`](../internal/tool/tool.go)：两大扩展接口。
10. [`internal/event/event.go`](../internal/event/event.go) 和
    [`internal/eventwire/wire.go`](../internal/eventwire/wire.go)：输出契约。
11. [`internal/agent/save.go`](../internal/agent/save.go) 和
    [`internal/store/session.go`](../internal/store/session.go)：落盘与恢复边界。
12. [`RECOVERY.zh-CN.md`](RECOVERY.zh-CN.md)、[`cmd/reasonix-guard`](../cmd/reasonix-guard) 和
    [`internal/repair`](../internal/repair)：不依赖正常桌面启动的应用级恢复边界。

第一遍只回答三个问题：谁创建谁、一次回合经过谁、事实由谁持久化。其余细节先做标记，不要
立即展开。

## 5. 五条纵向练习

### 练习 A：追踪一次普通 prompt

从 `cmd/reasonix/main.go` 出发，找到 `cli.Run`、`boot.Build`、Controller submit、
`runOrchestratedTurn`、`Agent.Run` 和 `Provider.Stream`。画出调用链，并标注：

- user text 在哪里变成 composed input；
- user message 在哪里追加到 session；
- `TurnStarted`、`Text`、`Usage`、`TurnDone` 从哪里发出；
- snapshot 发生在何时。

### 练习 B：追踪一个 `edit_file`

从 Tool Registry 找到 builtin，再沿 `executeBatch`/`executeOne` 标注：

- `ReadOnly` 如何影响并发；
- plan mode、permission、hook、checkpoint、sandbox 的顺序；
- diff preview 如何进入 approval/event；
- tool result 如何和 assistant tool call 配对。

### 练习 C：恢复一个 session

从 `config.SessionDir` 和 `store.Session*` helper 开始，找出 transcript、event log、index、goal、
checkpoint、jobs 和 lease。再比较 CLI resume 与 desktop exact-session open。目标是理解：

> session path 是运行身份；tab id 只是桌面展示身份。

### 练习 D：追踪桌面事件

按 `desktop/tabs.go` event sink → `internal/eventwire` → Wails `agent:event` →
`bridge.ts` → `useController.ts` → component 的顺序走一遍。重点观察：

- 事件为何附加 `tabId`；
- emitter 为何必须 FIFO；
- tab 切换时如何防止旧异步请求覆盖新 session；
- 哪些状态以 session、topic、workspace 或 tab 为 key。

### 练习 E：增加一个最小只读 Tool（练习分支）

实现一个无副作用的小工具并测试，但不急于提交。完成后检查：

- 是否实现全部 `tool.Tool` 方法；
- schema 是否稳定且 canonical；
- `ReadOnly` 是否真实；
- plan mode 是否需要显式分类；
- 工具合约和 cache guard 是否覆盖；
- 二进制入口是否确实注册了所在 builtin 包。

### 练习 F：区分会话恢复与应用恢复

先用临时 `REASONIX_HOME` 运行 `reasonix-guard check`，再阅读
`internal/repair` 的 snapshot、transaction、startup 和 update 路径。标出：

- `check` 为何默认只读，`repair` / `apply-plan` 如何生成可撤销事务；
- 启动状态如何从 `starting` 进入 `ready` / `healthy` / `clean-exit`；
- Safe Mode 为何不加载外部集成、不恢复 tabs，也不改写用户配置；
- 更新失败时为何必须回滚完整发布单元，而不是单个桌面二进制。

## 6. 构建和测试矩阵

### 6.1 根模块

```bash
make build            # CLI + plugin example 两个二进制
make vet
make test
go test -race ./...   # 并发、plugin、jobs 等改动时
```

常用聚焦方式：

```bash
go test ./internal/agent/ -run TestName -v
go test ./internal/control/ -run TestName -v
go test ./internal/tool/builtin/ ./internal/boot/
go test ./internal/repair/ ./cmd/reasonix-guard/
```

`make build` 不是 `go build ./...` 的别名；它按 Makefile 构建 CLI 和 plugin example。CI 还会
单独运行 `go build ./...`、跨平台 root tests、race、lint、cache guard 和 vuln scan。

### 6.2 桌面 Go 与前端

```bash
pnpm --dir desktop/frontend install --frozen-lockfile
pnpm --dir desktop/frontend build
pnpm --dir desktop/frontend test:all

make desktop-test
make desktop-test-short

cd desktop
go vet ./...
wails build -nocolour
```

根目录 `go test ./...` 会跳过嵌套的 `desktop/` 模块。桌面改动至少需要独立检查 Go、前端
typecheck/tests 和 Wails build；涉及 Windows path、WebView2 或原生窗口时，还要在 Windows
运行对应测试和程序。

### 6.3 Site / Workers

只在修改对应表面时运行：

```bash
cd site && npm test
cd workers/accounts && pnpm install --frozen-lockfile && pnpm typecheck
cd workers/forum && pnpm install --frozen-lockfile && pnpm typecheck
cd workers/crash-report && npm ci && npm run typecheck && npm test
```

部署和数据库迁移是外部状态变更，不属于普通代码验证；执行前必须按对应 workflow/README
确认环境和授权。

## 7. 按改动类型选择验证范围

| 改动 | 最小聚焦验证 | 合并前扩展验证 |
|---|---|---|
| 纯 leaf helper | 同包 tests | root vet/test |
| Tool 名称/描述/schema/顺序 | tool + boot + contract tests | root test，cache-impact guard |
| Provider 序列化/stream | provider tests | agent tests、root test、真实兼容端点 smoke test（若获授权） |
| Controller/turn flow | control + agent tests | root test，各前端事件/审批路径 |
| session/store | agent/store/control tests | resume/fork/delete/recovery，desktop/serve/ACP |
| Guard / Safe Mode / update rollback | repair + reasonix-guard + boot tests | config snapshot/undo，startup threshold，完整发布单元回滚 |
| desktop Go bindings | desktop Go tests | frontend typecheck/tests、Wails native build |
| desktop React state | 聚焦 TS/TSX test + typecheck | frontend full tests、Wails build、目标平台手测 |
| config/memory/skill/output style | 同域 + boot tests | cache guard、migration、user/project precedence |
| jobs/plugin 并发 | 同域 tests | `go test -race ./...` |
| release/workflow | YAML/script 静态检查 | canary 流程；不要把本地构建等同于发布验证 |

## 8. 四个高风险审查问题

### 8.1 Cache

- 是否改变 system prompt、standing memory、skill index、tool schema/顺序或 provider request？
- 能否把变化移到 turn tail？
- PR 是否填写 `Cache-impact`、`Cache-guard`，必要时填写 `System-prompt-review`？

### 8.2 权限

- Tool 的 `ReadOnly` 是否与真实 host side effect 一致？
- plan mode、permission、Guardian、hook、sandbox 是否仍各自生效？
- headless、sub-agent、YOLO 和 fresh-human-only 动作是否有独立测试？

### 8.3 Session 身份

- 状态究竟属于 workspace、topic、session 还是 tab？
- async result 返回时是否校验原 session path/sequence？
- 删除或 fork 是否覆盖所有 sidecar、lease 和后台任务？

### 8.4 跨平台

- 根 CLI 是否仍可 `CGO_ENABLED=0` 交叉编译？
- 桌面修改是否在原生 OS/Wails 环境验证？
- Windows 大小写折叠、路径分隔符、进程退出和 CRLF 是否有覆盖？

### 8.5 应用恢复

- 诊断是否保持默认只读，修复是否有快照、校验和可重试的 undo？
- Safe Mode 是否仍跳过配置/会话迁移、插件、Skills、MCP、hooks 与外部集成？
- 安装/更新变更是否对 desktop + Guard/启动器（或 macOS Bundle）做同一事务回滚？

## 9. 常见扩展工作流

### 9.1 新增内置工具

1. 在 `internal/tool/builtin` 实现 `Tool`。
2. 明确 `ReadOnly` 和可选的 Previewer、PlanModeClassifier、SnipHinter。
3. 用 `init()` 注册，检查重复名和稳定顺序。
4. 添加 schema/执行/错误/权限/plan mode 测试。
5. 同步 `TOOL_CONTRACT` 或确认现有生成/快照测试覆盖。
6. 做 cache-impact 审查。

### 9.2 新增 Provider

1. 在 `internal/provider/<kind>` 实现并注册 factory。
2. 处理文本、reasoning、tool-call delta、usage、image 和 retry 能力差异。
3. 在送往 API 前保持 tool call/result 配对和消息规范化。
4. 在实际二进制入口增加空导入并测试 config resolution。

### 9.3 新增前端能力

1. 先找 `control/port.go` 中最小端口。
2. 通用行为写在 Controller collaborator/domain package。
3. 用 `event.Event` 表达结果；JSON 前端走 `eventwire`。
4. 前端只实现命令映射、状态投影和展示。

### 9.4 修改持久化

1. 先定义权威数据和派生数据。
2. 路径统一放进 `internal/store`。
3. 同时设计 save、load、resume、fork、delete、crash recovery、lease/conflict。
4. 检查 CLI、desktop、serve、ACP 的调用面。

## 10. 桌面前端状态归属

新增 state 前先做归类：

| 类别 | 推荐位置 | 例子 |
|---|---|---|
| 可持久布局偏好 | Zustand `store/layout.ts` | panel 宽度、布局开关 |
| 瞬态全局浮层 | `store/overlays.ts` | modal/popover |
| 每 session 运行状态 | `useController` 的 tab state，但以 session identity 校验 | stream、tool cards、approval、todo |
| 明确按 topic 共享 | 带显式 topic key 的 helper | 当前 composer draft 语义 |
| 组件局部交互 | component local state | hover、当前展开项 |

不要仅因事件携带 `tabId` 就把所有数据都永久挂在 tab 上。当前 Composer draft 有意优先使用
topic identity，而 todo 优先 session path；改变它们是产品语义变更，不是无害重构。

## 11. 故障定位顺序

### 启动失败

1. 先区分是 Guard/安装发布单元问题，还是普通 Reasonix 启动问题。
2. 桌面端连续失败时先使用 Guard 只读 `check`；不要绕过系统恢复对话框反复强启。
3. `go.mod` 是否能解析，工具链、workspace root 与 `REASONIX_HOME` 是否如预期。
4. 普通模式下再查配置来源、model reference、Provider key、plugin 启动和 schema quarantine。
5. Safe Mode 可启动而普通模式不可启动时，继续比较被安全模式跳过的配置、迁移和外部集成；最后再看 frontend。

### 模型没有调用工具

1. Tool 是否进入 registry，名称/schema 是否发送给 Provider。
2. token/capability profile 是否隐藏或代理该工具。
3. plan mode 或 delivery readiness 是否阻断。
4. permission/hook/Guardian 是否拒绝。
5. Tool result 是否正确配对写回 session。

### 桌面显示错乱或串 session

1. 事件中的 `tabId` 和当前 `sessionPath`。
2. tab/topic/session 是否发生 rebind。
3. stale async load 是否被 sequence guard 拦截。
4. `runtime:rebuilt` 是否与 agent events 同队列。
5. frontend state key 和 persisted key 属于哪个身份层。

### 会话“删除后复活”

1. 主 transcript 是否删除。
2. authoritative `.events.jsonl` 是否仍在。
3. index、meta、goal、checkpoint、jobs、lease 是否按生命周期处理。
4. cleanup-pending reconciler 是否完成。

## 12. 推荐的前两周节奏

### 第 1～2 天：主链

完成练习 A/B，只读 core package 和对应 tests，能解释一次回合与一次工具调用。

### 第 3～4 天：状态和安全

完成练习 C/F，分清 session sidecars/checkpoint 与 Guard/repair，并理解
config precedence、permission/plan/sandbox 和 compaction。

### 第 5～6 天：选择一个前端

若主要维护桌面，完成练习 D；若主要维护 CLI/服务端，则追踪 TUI 或 serve 的命令和事件映射。

### 第 7～8 天：完成一个小修复

选择有现成测试接缝、无 schema/持久化迁移的小问题。先补失败测试，再改实现，跑最小矩阵和
root checks。

### 第 9～10 天：做一次跨层变更演练

可在练习分支完成 Tool 或事件字段的端到端变更，写清 cache、权限、wire、session 和平台影响，
不一定合并。

## 13. PR 交接清单

- [ ] 改动落在正确层，未在某一前端复制通用逻辑。
- [ ] 新接口有编译期实现检查或测试。
- [ ] Tool/Provider/event wire 的模型或前端可见契约已同步。
- [ ] Cache 影响已评估并填写 PR 元数据。
- [ ] 权限、plan、headless、sub-agent 路径按风险覆盖。
- [ ] session 变更覆盖所有 sidecar 和删除/恢复路径。
- [ ] Guard / Safe Mode / updater 变更保持只读诊断、可撤销修复和完整发布单元回滚。
- [ ] 根模块与嵌套 desktop/Node 模块分别验证。
- [ ] 并发变更运行 race tests。
- [ ] 文档没有把计划、推断或子系统版本写成全产品已确认事实。
- [ ] `git diff --check` 通过，diff 中没有生成物、CRLF 噪声或无关文件。

## 14. 接下来按主题深入

- 用户和配置：[`GUIDE.zh-CN.md`](GUIDE.zh-CN.md)、[`CONFIG_PATHS.zh-CN.md`](CONFIG_PATHS.zh-CN.md)
- CLI：[`CLI.zh-CN.md`](CLI.zh-CN.md)
- Tool 契约：[`TOOL_CONTRACT.zh-CN.md`](TOOL_CONTRACT.zh-CN.md)
- Session memory/history：[`SESSION_MEMORY_RETRIEVAL.md`](SESSION_MEMORY_RETRIEVAL.md)
- Checkpoint：[`CHECKPOINTS.md`](CHECKPOINTS.md)
- 启动恢复与安全模式：[`RECOVERY.zh-CN.md`](RECOVERY.zh-CN.md)
- 子智能体：[`SUBAGENT_PROFILES.zh-CN.md`](SUBAGENT_PROFILES.zh-CN.md)
- 插件包：[`PLUGIN_PACKAGES.zh-CN.md`](PLUGIN_PACKAGES.zh-CN.md)
- 在线服务与发布生态：[`ECOSYSTEM.zh-CN.md`](ECOSYSTEM.zh-CN.md)
- 发布：[`RELEASING.md`](RELEASING.md)
- 桌面构建：[`desktop/README.md`](../desktop/README.md)
