# 桌面宿主协议

[English](DESKTOP_HOST_PROTOCOL.md)

Electron 壳与 Go 桌面服务是两个进程，通过服务进程 stdio 上的一条私有、带版本的
JSON-RPC 2.0 连接协作。本文是双方共同实现的契约：Go 侧拥有全部桌面业务命令，
Electron 侧拥有全部原生界面。任何一方都不得绕过契约：React 界面不触碰 Electron
或 Go 全局对象，Go 业务代码不链接任何壳工具包。

```text
React 渲染进程 ──preload 类型化 IPC──▶ Electron 主进程 ──stdio JSON-RPC──▶ Go 桌面服务
                                          ▲                                   │
                                          └────── host/* 反向请求 ────────────┘
```

## 传输

- 帧格式：按行分隔的 JSON-RPC 2.0（`rpcwire` 严格模式）。一行一帧，UTF-8，不允许批量数组。
- Go 服务以 `reasonix-desktop --host-rpc` 启动。stdout 只承载协议帧，stderr 承载日志。
  壳在 `desktop/shutdown` 之后关闭 stdin 以请求退出。
- 限制：双向单帧 64 MiB，服务端最多 512 个并发入站处理器，30 秒写入停滞看门狗。
  大体积二进制数据从不进入帧，而是走下文的资源源。
- 壳发出的每个请求都在独立 goroutine 上执行，与已退役的进程内壳的绑定调用一致。只有
  `desktop/event` 帧保证顺序，它们由服务端的单一队列写出。

## 握手

新连接上的第一个请求必须是 `desktop/hello`，否则以 `-32002 not_ready` 失败。

```jsonc
// 壳 → 服务
{"method":"desktop/hello","params":{
  "protocolVersion": 1,
  "contractDigest": "sha256:…",       // 壳包内嵌的契约摘要
  "build": {"version":"v1.30.0","channel":"stable","commit":"abc123"},
  "host": {"name":"electron","version":"44.2.0","chrome":"152.0.0","platform":"darwin","arch":"arm64"},
  "instance": {"home":"/Users/…/.reasonix","dev":false}
}}
// 服务 → 壳
{"result":{
  "protocolVersion": 1,
  "contractDigest": "sha256:…",
  "service": {"version":"v1.30.0","channel":"stable","commit":"abc123","pid":4242},
  "runtimeGeneration": "g-01J…",       // 每个服务进程唯一
  "resources": {"origin":"http://127.0.0.1:51234","token":"…"},
  "window": {"width":1280,"height":820,"minWidth":760,"minHeight":480,"frameless":false,"zoomFactor":1}
}}
```

`window` 是 Go 根据保存状态和平台规则得到的主窗口初始几何。可选
`position: {x, y}` 传递保存的原点（零坐标和负坐标均有效），缺省表示居中。
壳选择匹配的显示器，按其 DIP 可用区域校正矩形后隐藏创建窗口。Go 随后在
`domReady` 中最大化并显示，不再覆盖壳校正后的位置。持久化始终采集普通状态
矩形，最大化标记单独保存；旧的超大矩形会按可用区域修正，不会将所有最大化
记录都重置为默认尺寸。
最小化期间，壳保留最后一次非最小化快照，避免原生普通矩形查询返回最大化外框。

持久化 JSON 结构不变。旧壳忽略握手中可选的位置字段，新壳接受字段缺失。
壳与服务应配套发布：开发模式混用不同版本不能提供完整的恢复修复。降级可能
重新引入旧几何问题，旧版读取器也可能拒绝低于其原有校验下限的负坐标。

失败码都是终止性的：壳显示真实错误，提供“打开日志”和“退出”，绝不回退到浏览器 mock。

| 代码 | 名称 | 含义 |
| --- | --- | --- |
| `-32001` | `protocol_mismatch` | `protocolVersion` 不一致 |
| `-32003` | `contract_mismatch` | 命令/事件摘要不一致（混装） |
| `-32004` | `build_mismatch` | 壳与服务版本不同且都不是 `dev` |
| `-32005` | `instance_mismatch` | 壳的规范数据目录与服务的不一致 |
| `-32002` | `not_ready` | hello 成功前的请求 |

`runtimeGeneration` 标记该服务进程发出的每个事件、每个审批和浏览器授权。服务重启
后产生新的世代；壳丢弃任何旧世代标记的内容。

## 生命周期请求（壳 → 服务）

| 方法 | 参数 | 结果 | Go 负责者 |
| --- | --- | --- | --- |
| `desktop/start` | `{}` | `{}` | `App.startup` |
| `desktop/domReady` | `{}` | `{}` | `App.domReady` |
| `desktop/rendererAttached` | `{"rendererGeneration":n}` | `{}` | 前端心跳/就绪 |
| `desktop/beforeClose` | `{"reason":"window"\|"quit"\|"tray"\|"updater"}` | `{"prevent":bool}` | `App.beforeClose` |
| `desktop/shutdown` | `{}` | `{}` | `App.shutdown` |
| `desktop/hostEvent` | `{"name":string,"payload":any}` | `{}` | 第二实例、托盘打开/退出、菜单动作 |
| `desktop/browserControl` | `{"enabled":bool}` | `{}` | 内置浏览器开关，构建会话时读取 |

顺序：`hello` → `start` → 窗口加载 → `domReady` →（每次渲染进程挂载后 `rendererAttached`）
→ … → `beforeClose` →（`shutdown` → 关闭 stdin → 退出）。无论是否调用过 `shutdown`，
stdin 关闭后服务都会自行退出，因此壳突然死亡不会留下无头 Go 进程。

## 业务命令

```jsonc
{"method":"desktop/invoke","params":{"method":"OpenProjectTab","args":["/path", true]}}
{"result": {...}}                                  // 方法的 JSON 结果，void 为 null
{"error":{"code":-32000,"message":"<错误文本>","data":{"method":"OpenProjectTab"}}}
```

`method` 必须是契约注册表接受的 Go `App` 导出方法。签名沿用已退役壳的规则：任意可
JSON 序列化的参数，结果为 `()`、`(T)`、`(error)` 或 `(T, error)`。注册表在构建期拒绝
其他形态，因此接口面不可能出现壳无法调用的方法。壳在转发前按内嵌命令表校验
`method`；未知名称以 `-32601` 失败。

生成的契约（`cd desktop && go run . -emit-contract frontend/src/generated`）是唯一
事实来源：它输出 JSON 契约、摘要、TypeScript 命令表和渲染进程使用的 DTO 类型声明。
检入的输出漂移时桌面 Go 测试失败。

每个命令还记录源码模块 `domain`、准确的 `owner`（例如 `App.OpenProjectTab`）、
仓库相对路径 `sources`、`scope` 和 `cancellation`，这些字段共同参与摘要。
生成器扫描所有平台声明并输出 `desktop/host_command_owners.generated.json`；宿主内嵌
该文件，要求每个反射命令都有匹配元数据。Scope 记录原方法的命名 wire `inputs`
（遗留无名参数使用 `argN`）和 `resolver`；无参数命令使用 `owner-state`，其余使用
`owner-inputs`。这些字段描述来源和分派边界；输入校验、标签/会话选择及权限检查仍由
原有 App 方法负责。

当前 App 命令声明 `before-dispatch`：宿主在解码前及真正分派前检查取消；同步写入
开始后，即使收到取消也保留原方法的结果，不承诺中断已分派的方法。宿主方法可通过
首个 Go 参数 `context.Context` 声明 `cooperative-context`；宿主注入请求 context，
它不属于 JSON 参数或生成的 DTO。方法自身必须配合取消。业务 Stop/Cancel 命令继续
沿用已有 owner 和语义。

## 事件（服务 → 壳 → 渲染进程）

```jsonc
{"method":"desktop/event","params":{"seq":1093,"generation":"g-01J…","name":"agent:event","args":[{...}]}}
```

`args` 保留原事件桥的可变参数载荷，多数事件只有一个元素。壳把该帧经
`reasonix:event` 通道转给渲染进程；preload 的 `on(name, cb)` 按 `name` 过滤并调用
`cb(...args)`。序号在同一世代内严格递增，重新挂载的渲染进程可据此发现缺口并重新
快照，而不是信任陈旧状态。

服务监督器和 preload 都拒绝重复、倒序帧；preload 还根据当前服务状态拒绝旧世代帧，
并在 React 订阅前接入传输。世代变化、序号缺口或订阅期间遗漏会触发壳内事件
`desktop:resync`（`generation`、`reason`、`expectedSeq`、`actualSeq`），它不属于 Go
业务事件。运行状态通过 `SyncRuntimeState` 重读；已挂载控制器重读 `ListTabs`，复用
现有 `TurnEventsForTab` 日志与待审批提示恢复投影。异步读取受后续恢复请求、会话身份
和导航变更约束，不重放业务调用。服务重启后复用仍存活的应用渲染进程，保留未发送草稿。

当前保证范围是核心运行状态、会话元数据、持久化轮次事件和待审批提示。终端虽有有界
输出快照，但没有原子输出游标，因此缺口后的终端会明确标记输出可能不完整，不将无法
确定边界的快照合并进实时输出。扩展输出、文件监听等独立事件流仍需各自的重新快照契约，
不在上述核心恢复保证范围内。

## 原生宿主调用（服务 → 壳）

这些调用替代 Go 中对壳工具包的直接调用。每一项对应 Go `nativeHost` 接口的一个方法；
Wails 实现已随 Electron 壳落地删除。

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `host/window.show` | `{"reason":string}` | `{}` |
| `host/window.hide` | `{}` | `{}` |
| `host/app.hide` | `{}` | `{}`（macOS 应用级隐藏） |
| `host/window.maximise` `unmaximise` `minimise` `unminimise` `toggleMaximise` `center` | `{}` | `{}` |
| `host/window.isMaximised` `isMinimised` | `{}` | `{"value":bool}` |
| `host/window.setPosition` | `{"x":n,"y":n}` | `{}` |
| `host/window.setTitle` | `{"title":string}` | `{}` |
| `host/screen.list` | `{}` | `{"screens":[{"x","y","width","height","scale","primary"}]}` |
| `host/dialog.openDirectory` | `{"title","defaultDirectory"}` | `{"path":string}`（`""` 表示取消） |
| `host/dialog.openFile` | `{"title","defaultDirectory","filters":[{"displayName","pattern"}],"multiple":bool}` | `{"paths":[]}` |
| `host/dialog.saveFile` | `{"title","defaultDirectory","defaultFilename","filters"}` | `{"path":string}` |
| `host/dialog.message` | `{"type":"info"\|"warning"\|"error"\|"question","title","message","buttons":[],"defaultButton","cancelButton"}` | `{"button":string}` |
| `host/shell.openExternal` | `{"url":string}` | `{}` |
| `host/app.quit` | `{}` | `{}` |
| `host/app.relaunch` | `{"args":[],"execPath"?:string}` | `{}` |
| `host/devtools.toggle` | `{}` | `{}` |
| `host/remoteWindow.open` | `{"hostKey","url","title"}` | `{"windowId":string}` |
| `host/remoteWindow.navigate` | `{"hostKey","url","title"}` | `{}` |
| `host/remoteWindow.focus` `close` | `{"hostKey"}` | `{}` |
| `host/tray.ensure` | `{"openTitle","openTooltip","quitTitle","quitTooltip","tooltip"}` | `{"ready":bool,"reason":string}` |

`host/shell.openExternal` 只接受 `http:`、`https:` 和 `mailto:` URL。
包括 `file:`、`javascript:`、`data:` 在内的其他协议会在 Electron 宿主边界
被拒绝，不会交给系统打开器执行。
| `host/tray.destroy` | `{}` | `{}` |
| `host/browser.grant` `revoke` | `{"grantId","tabId","sessionId"}` / `{"grantId"}` | `{}` |
| `host/browser.tabs.list` | `{"grantId"}` | `{"tabs":[{"id","url","title","loading","temporary"}]}` |
| `host/browser.tabs.open` | `{"grantId","url","temporary"}` | tab |
| `host/browser.tabs.navigate` | `{"grantId","tabId","url","action"}` | tab |
| `host/browser.tabs.close` | `{"grantId","tabId"}` | `{}` |
| `host/browser.snapshot` | `{"grantId","tabId","selector"}` | `{"documentToken","url","title","tree","refs"}` |
| `host/browser.act` | `{"grantId","operationId","tabId","documentToken","action","ref","text","keys","options","files","submit","deltaX","deltaY"}` | `{"executed","reason","documentToken"}` |
| `host/browser.screenshot` | `{"grantId","tabId","ref","fullPage","directory"}` | `{"path","mime","width","height"}` |
| `host/browser.downloads` | `{"grantId","tabId","waitForMs"}` | `{"downloads":[{"id","url","path","state","bytes"}]}` |

浏览器调用以 `-32010`（引用过期）、`-32011`（用户已接管该标签）或 `-32012`（没有当前授权）
失败；Go 执行器把它们映射到内核哨兵错误，并把操作结果记入账本。授权的 `tabId` 是桌面
标签（任务）；该授权下打开的浏览器标签归属于它。

宿主事件（`desktop/hostEvent`）：`tray.open`、`tray.quit`、`secondInstance`（`payload` 携带
原始 argv）、`menu.showWindow`、`remoteWindow.closed`（`{"hostKey"}`）、`browser.takeover`
（`{"tabId","epoch","reason"}`）。

对话框结果从不暴露文件内容；它们只返回路径，再由 Go 经现有工作区和媒体检查授权。

## 资源源

服务在回环端口上监听，承载现有的授权资源处理器（`/__reasonix_workspace_media/…`、
`/__reasonix_theme_asset/…`、远程 markdown 图片代理）。壳从受限的 `reasonix://app/`
scheme 提供打包界面，只把上述前缀转发到资源源，并在主进程中附加
`Authorization: Bearer <token>`。token 从不到达渲染进程、网站视图、远程窗口或
MCP App 框架。Go 保留今天的全部文件身份与 TTL 检查。

## 渲染进程 preload 接口

可信 preload 只暴露一个对象 `window.reasonixDesktop`：

```ts
interface ReasonixDesktopHost {
  readonly kind: "electron";
  readonly contract: { protocolVersion: number; digest: string; commands: readonly string[] };
  readonly platform: { os: "darwin" | "windows" | "linux"; arch: string; versions: Record<string, string> };
  invoke(method: string, args: unknown[]): Promise<unknown>;
  on(name: string, cb: (...args: unknown[]) => void): () => void;
  native: {
    openExternal(url: string): Promise<void>;
    clipboard: { writeText(text: string): Promise<boolean>; readText(): Promise<string> };
    window: {
      setTheme(theme: "system" | "light" | "dark"): void;
      setBackgroundColour(r: number, g: number, b: number, a: number): void;
      getBounds(): Promise<{ x: number; y: number; width: number; height: number; maximised: boolean }>;
      isMaximised(): Promise<boolean>;
      minimise(): void; toggleMaximise(): void; close(): void;
    };
    getPathForFile(file: File): string;          // 原生拖放路径
    onServiceState(cb: (state: ServiceState) => void): () => void;
    browserControl: {                            // 内置浏览器的设置页
      get(): Promise<BrowserControlState | null>;
      setEnabled(enabled: boolean): Promise<BrowserControlState>;
      setIgnoreCertificateErrors(enabled: boolean): Promise<BrowserControlState>;
      clearCache(): Promise<void>;               // 保留 Cookie 与站点数据
      clearAllData(): Promise<void>;             // Cookie、站点数据与缓存
      importChromeLogin(): Promise<ChromeImportOutcome>;
    };
  };
  browser: {                                       // 用户驱动的浏览器面板；Agent 调用经 Go
    list(): Promise<BrowserTabView[]>;
    open(url: string, opts?: { temporary?: boolean; taskId?: string }): Promise<BrowserTabView>;
    close(tabId: string): Promise<void>;
    activate(tabId: string | null): Promise<void>;
    navigate(tabId: string, target: { url?: string; action?: "back" | "forward" | "reload" | "stop" }): Promise<void>;
    setZoom(tabId: string, factor: number): Promise<void>;
    toggleDevTools(tabId: string): Promise<void>;
    resume(tabId: string): Promise<void>;          // 把接管的标签交还给 Agent
    setLayout(rect: { x: number; y: number; width: number; height: number } | null): void;
    setOverlay(active: boolean): void;             // 应用覆盖层隐藏所有网站视图
    onTabs(cb: (tabs: BrowserTabView[]) => void): () => void;
    onDownload(cb: (download: BrowserDownloadView) => void): () => void;
  };
}
```

`BrowserTabView` 为 `{ id, taskId, url, title, loading, canGoBack, canGoForward,
temporary, mode: "agent" | "human", epoch, zoom, error }`，`BrowserDownloadView` 为
`{ id, tabId, url, filename, path, state, received, total }`。网站视图位于
`persist:browser`（共享登录）或 `temp:<id>` 分区，永远不会获得应用 preload。

`ServiceState` 为 `{ phase: "starting" | "ready" | "restarting" | "failed" | "exited"; generation: string; error?: string }`。
业务组件只导入类型化 SDK，从不直接使用该对象；只有桥接适配层读取它。

`BrowserControlState` 为 `{ controlEnabled, ignoreCertificateErrors, writable,
warning: "invalid-config" | "unreadable-config" | "unsupported-version" | null }`，
`ChromeImportOutcome` 为 `{ ok: true, profile, cookies, skipped }` 或
`{ ok: false, reason }`，`reason` 取值 `chrome-missing`、`profile-not-found`、
`cookies-unreadable`、`safe-storage-denied`、`safe-storage-unavailable`、
`unsupported-platform`。

## 安全边界

- 应用窗口：sandbox 开启，context isolation 开启，Node integration 关闭，只加载
  `reasonix://app`，使用上述 preload。
- 网站视图、远程 Serve 窗口和 MCP App 框架：独立 session，没有应用 preload，不能访问
  `reasonix://`，不能触达 `host/*`。
- IPC 处理器只接受来自应用窗口 `webContents` 的请求，其他发送者被拒绝并记录。
- 内嵌契约之外的 `desktop/invoke` 名称在到达 Go 之前失败。

## 性能诊断补充

以下可选 native 接口仅允许可信应用主框架调用。旧 shell 可缺少这些接口；
不涉及持久化用户数据格式变更或迁移。

- `processDiagnostics()` 返回 `{scope: "electron", samples, growth}`。
  样本包含年龄、可空 CPU 区间、PID/类型/创建时间、可空 CPU 百分比、
  工作集及私有内存（MiB）、截断标记。前台最多每 30 秒采集一次，后台每 60 秒一次；
  最多保留五分钟内的 12 条记录，每条最多 128 个进程。不采集标题、URL 或进程名称。
  范围仅含 Electron 管理的进程，不含 Go 服务。
- `captureRendererProfile(requestId?)` 通过 CDP 录制当前 renderer 五秒，
  请求的采样间隔为 10ms，返回状态、时长和最多八个应用脚本的自身耗时摘要。
  普通页面不启用 JS Self-Profiling。最多一个进行中的采样，要求窗口在前台，
  冷却十分钟，每次启动 shell 最多尝试三次。不接管已有 debugger/DevTools。
  失焦、隐藏、导航、renderer 退出或取消会停止采样。
- `cancelRendererProfile(requestId)` 仅取消身份匹配的采样；忽略 renderer 不带身份的取消请求，
  防止长时间挂起后迟到的旧请求干扰新采样。每条 CDP 命令最多等待 1.5 秒，
  所有终态均释放自己的 debugger。分析在临时 Worker 中运行，老生代限制 32 MiB，
  超时 1.5 秒，输入最多 20,000 个节点及 100,000 个样本。
  原始 profile 不进入 UI 报告。
- `exportHeapSnapshot()` 先显示原生风险提示并要求用户确认，再选择保存路径，
  仅保存本地、不上传，也不接受 renderer 提供的路径。快照可能包含代码、聊天、
  密钥，会暂停界面并可能占用较多磁盘。Electron 无法中断已开始的快照，
  因而忙碌状态保持到操作实际结束，不用超时伪装取消成功。

内存增长信号要求 PID 与创建时间连续一致，至少五次读数跨越两分钟，
最近三次读数均超过前两次的较高基线至少 256 MiB 且至少 50%。
全程可用时采用私有内存，否则使用工作集。这表示观察到持续增长，
不代表确认泄漏，也不代表独占的物理内存。

报告立即显示已有证据。进程补充最多等待 750ms；短时 CPU 采样结束后更新同一报告。
前端 12 秒后放弃采样补充并请求取消。这些是异步等待期限，不是同步工作可被抢占的保证。
迟到结果不会重建已经关闭的报告，采样结果明确标注为触发后的数据。
用户主动生成堆快照期间及结束后五秒内，暂停压力报警，避免诊断触发自身报警。

在 `desktop/electron` 运行 `node scripts/performance-smoke.mjs`，
可用隔离原生测试验证采样 owner、Worker、报告更新及本地堆快照。
`node scripts/performance-benchmark.mjs` 分别以关闭监测、基础监测、短时采样运行
三次独立进程对照，记录可用的 CPU 时间、帧时序、工作集及指标采集耗时，
各模式使用同一 renderer bundle，通过运行时开关选择；固定活动信号并关闭后台节流，
用于无人值守比较成本。宿主事件测试独立覆盖生产焦点和导航取消策略，
原生 smoke 验证实际 CDP 与 ASAR 路径。
输出至 `artifacts/performance/overhead.json`。该合成测试不等同于 Windows 用户场景复现；
仍需对比刚启动、长时间使用、切回窗口和关闭标签等阶段。
