# Reasonix Mobile — Android 壳

把 Reasonix 装进一个安卓 App。内核随包分发、在本机启动，界面是同一个
`desktop/frontend-next` 构建产物，AI 通过 SSH 操控服务器。

这里写**构建者**需要的东西；面向使用者的说明（装什么、怎么连服务器、已知限制）见发行说明。

## 一条命令

```bash
./scripts/build.sh release     # 或 debug
```

依次做三件事，顺序不能变：

1. `build-kernel.sh` → `app/src/main/jniLibs/arm64-v8a/libreasonix.so`
2. `sync-page.sh` → `app/src/main/assets/page/`
3. `./gradlew assembleRelease`

产物：`app/build/outputs/apk/release/app-release.apk`

**内核和页面是打包的输入，不是 Gradle 的构建步骤。** 直接跑
`gradle assembleRelease` 会明确报错、并告诉你缺哪个文件该跑哪个脚本 —— 这是
`requireBuildInputs` 的用途：宁可失败，也不要产出一个没有内核的 APK。

## 需要的环境

| 组件 | 本机路径 | 版本 |
| --- | --- | --- |
| JDK | `%JAVA_HOME%` | 21（17 也可） |
| Android SDK | `%ANDROID_HOME%` | `platforms;android-35` + `build-tools;35.0.0` + `platform-tools` |
| Gradle | 用仓库自带 wrapper | 8.14.3（AGP 8.11.1 配套） |
| Go | `%LOCALAPPDATA%\Programs\Go\bin` | 1.26+ |
| Node / pnpm | 系统 | Node 24+ / pnpm 10 |

构建前设好：

```bash
export ANDROID_HOME="$HOME/Android/Sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export JAVA_HOME="/path/to/jdk-21"
export PATH="$PATH:$(go env GOPATH)/bin"
export GOPROXY=https://goproxy.cn,direct   # 国内拉 Go 依赖
```

`scripts/build.sh` 会自动装前两项之外的东西；`ANDROID_HOME` 必须自己设。

## 脚本

| 脚本 | 作用 |
| --- | --- |
| `build.sh [debug\|release]` | 全流程出包 |
| `build-kernel.sh` | 只交叉编译内核（`GOOS=android GOARCH=arm64 CGO_ENABLED=0`） |
| `sync-page.sh` | 只构建前端并把 `dist/` 复制进 assets |
| `verify-page.sh` | **不需要手机**：用打包的页面 + 真实内核跑一遍启动链路 |
| `run.sh [debug\|release]` | `adb install` + 启动 + 跟日志 |

`verify-page.sh` 是改完东西后最该跑的一条：它用 `Kernel.kt` 相同的启动参数
（`--auth token`、`--token-file`、`--port-file`、`-page`）把内核拉起来，
确认能绑端口、能返回 200。手机不在手边时，这是能验到的最深处。

## 代码在哪

| 文件 | 管什么 |
| --- | --- |
| `MainActivity.kt` | WebView、返回键、下载、外链、渲染进程重建 |
| `Kernel.kt` | 解包页面、启停内核、等端口、健康探测、日志、工作区目录与历史迁移 |
| `KernelService.kt` | 前台服务保活（长时间任务不被系统杀掉） |
| `NativeBridge.kt` | `@JavascriptInterface`：导入私钥、开外链、剪贴板、系统主题 |
| `Downloader.kt` | 把内核产出的文件写进系统文档选择器 |

页面侧通过壳层注入的 `window.reasonixHost` 与 `ReasonixNative` 接进来；
这两者的定义在 `desktop/frontend-next/src/port/host.ts` 的 `AndroidHost`。

**手机版与桌面版共用同一份页面代码**（`desktop/frontend-next`），
所以「只在手机上不同」的东西全部靠运行时判断，没有第二套 JSX：

| 判断 | 读什么 | 用在哪 |
| --- | --- | --- |
| `phoneShell()`（`src/port/host.ts`） | `<html data-shell="android">`，由 `index.html` 在 bundle 之前设好 | 组件要不要渲染某一支（如设置里的「手机访问」面板） |
| `[data-shell="android"]`（`mobile.css`） | 同一个属性 | 全部手机端样式 |

同一个属性喂两边，样式与组件才不会对「这是不是手机」给出不同答案。

| 文件 | 管什么 |
| --- | --- |
| `src/ui/pairings.ts` | 扫过的电脑（origin + 时间）存 `localStorage`，供扫码菜单的历史重连。存的是 **origin** 而不是扫到的链接：`#pair=` 里的码是一次性的，扫一次就作废，靠 `#token=`/`#pair=` 重开同一个链接什么也不会发生 |
| `src/ui/ScanToConnect.tsx` | 顶栏扫码按钮：点开是菜单（扫码 + 历史），不是直接开相机 |

## 实现笔记

下面按区域记下这个壳在安卓上真正踩到的东西 —— 多数是「桌面测不出来、只有真机才现」的那一类，留在这里省下一次重踩。

### 构建

**目录名带中文。** AGP 默认拒绝在非 ASCII 路径下构建（它担心 `aapt2` 的代码页），
所以 `gradle.properties` 里有 `android.overridePathCheck=true`。在纯 ASCII 路径下这一行可以删掉。

**若 `~/.gradle/init.gradle` 里配了镜像源。** 如果 `settings.gradle` 里写
`RepositoriesMode.FAIL_ON_PROJECT_REPOS`，那个脚本会让构建直接失败。这里没有 fail-on，两个来源可以共存。

## 打包进 APK 的内容

| 内容 | 大小 | 为什么是这个样子 |
| --- | --- | --- |
| `lib/arm64-v8a/libreasonix.so` | 45 MB | Go 静态二进制，已 `-s -w` 去符号。它是 `.so` 而不是 `assets/` 里的文件，因为只有 `nativeLibraryDir` 下的文件安卓保证可执行 |
| `assets/page/` | 14 MB | 前端构建产物（含 Inter/Noto Sans SC 字体），已 `noCompress` |
| `assets/LICENSE` | 1 KB | 上游 MIT 许可原文，**逐字未改**。MIT 要求二进制分发同样随附版权声明与许可全文，所以它必须进包 |

**`assets/LICENSE` 要放在 `assets/` 根，不能放进 `assets/page/`。** `sync-page.sh`
每次构建都 `rm -rf` 那个目录再重建，放进去会被清掉、且不报错 —— 包照出，许可没了。
它来自仓库根的 `LICENSE`，更新上游后应同步复制一份。

release 包 31 MB，主要是前两块压在一起的结果。

## 页面的浏览器视图

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `Pane.tsx` + `BrowserPanel.useBrowserTabs` | 页签列表只在收到 `browser_tabs_changed` 那一帧时重读；漏掉该帧后列表永远停在旧值，工作台里的浏览器页签整块消失，只有重启才恢复。 | `useBrowserTabs` 新增 `visible` 参数并放进 effect 依赖，工作台（`tab === "browser"` 或停靠中的 `docked`）打开时重读一次。 |
| `BrowserPanel.AgentBrowserView` 的 `place()` | 面板刚显示的那一帧容器尚未排版，`visibleBox()` 返回 null，旧代码直接 `hideBrowserView()`，之后靠 400ms 轮询找回。 | 量到空框时先 `schedule()` 重试一帧（`misses` 计数），第二帧仍为空才收起来。 |
| `MainActivity.showBrowserView` | 收到 0×0 矩形会照常 `layoutParams` 并 `VISIBLE`，把视图摆成零尺寸。 | 开头 `if (w <= 0 || h <= 0) return`，保留上一尺寸等下一次真实矩形（`handleHostCall` 仍会 `replyHost`，页面的 Promise 不受影响）。 |

## 浏览器：导航失败与元素定位

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `BrowserShellServer` 构造 | `/load` 只发起导航就返回，失败槽位跨导航残留 → 下一次 `/read` 返回上一次的旧错误。 | 构造新增 `startNavigation` / `awaitNavigation` / `requests` / `loadFailure` 参数；`/load` 先 `startNavigation()`；`/read` 先 `awaitNavigation(NAV_SETTLE_MS=10s)` 再依据 `wv.url` 是否 `chrome-error:` 判定，脚本等待用 `READ_SCRIPT_MS=15s`（**两者之和 25s 必须低于内核 30s 超时**）。 |
| `MainActivity`（浏览器导航） | 同上。 | 新增 `browserNavLatch` / `browserRequests` / `browserShellLock`；`onPageFinished` 调 `settleBrowserNavigation()`；`shouldInterceptRequest` 记请求（最多 `maxBrowserRequests` 条）。 |
| `BrowserShellServer` 快照脚本 | 不产生元素 ref，`fill`/`click` 无法定位。 | 快照脚本给可交互元素打 `data-rx-ref`，响应新增 `elements`；新增 `POST /requests`、`/click` 返 `{ok}`。 |
| `BrowserShellServer.handleScreenshot` | 异常时返 200 带错误体，内核当成空截图。 | 用 `AtomicBoolean` 标记，异常返 500。 |
| `Kernel` | 内核语言恒为英文（安卓不设任何 POSIX locale 变量）。 | 注入 `REASONIX_LANG=Locale.getDefault().toLanguageTag()`（内核 `envCandidates()` 的第一候选）。 |
| 内核 `platform/browser/shell.go` | 快照不含 ref、失败不带原因、导航后旧 ref 仍有效。 | 渲染 ref 行；失败按 `reason` 报 `CodeNavigationFailed`；新增 `forgetRefs()` 并在 load/CloseTab/back/forward/reload 五处调用；`Requests()` 标 `ResponseUnknown`；`alive()` 补 Bearer 头。 |
| 内核 `tools/builtin/browser.go` | 失败只有英文码；`new_tab` 无提示。 | `sayBrowserFailure` 走 `i18n.M`；`newTabNote` 说明单视图行为；`click` 未命中改报 `CodeStaleRef`。 |
| `android/scripts/build-kernel.sh` | 内核版本恒为 `dev`。 | 从 `app/build.gradle` 读版本，注入 `-X main.version` / `linkedVersion` / `linkedRevision`。 |

## 浏览器：加载失败、地址同步与窄屏

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `MainActivity.parseChooserResult`（新） | `FileChooserParams.parseResult` 在结果**不含** `Intent.getData()` 时返回 null，而多选恰好就是这样到达的（选项在 `clipData`、单元素 `data` 未设）→ 「多选 = 一个都没添加」、单选正常。 | 自有解析：先读 `data.clipData` 逐项取 uri，再回退 `data.data`，`LinkedHashSet` 去重。 |
| `MainActivity`（浏览器视图） | 加载失败后 WebView 会导航到自己的错误页 `chrome-error://chromewebdata/`，**那也是一次导航** —— `onPageStarted` 把刚置上的失败标志清掉，`onPageFinished` 又上报成功。 | `onPageStarted` 只在 url **不以 `chrome-error:` 开头**时才清 `mainFrameFailed` / `browserLoadFailure`；`onPageFinished` 也要求当前 url 不是错误页。 |
| `BrowserShellServer` 构造 | `/read` 从错误页抽 `innerText`，把浏览器自己的「网页无法打开」当站点正文交给内核。 | 新增第 3 个参数 `loadFailure: () -> Pair<String, String>?`；`/read` 见失败即返回 `{url, reason, text:""}`。 |
| `MainActivity.browserLoadFailure`（新） | 同上，壳层需要记住当前失败。 | `@Volatile` 字段，`onReceivedError`（主框架）记录，`onPageStarted`（正常导航）清除。 |
| 前端 `state/prefs.ts` | 窄屏下 agent 打开网页会抢占整屏。 | 新增 `followsBrowser()` / `setFollowsBrowser()`（键 `rx-browser-follow`）；未保存时按 `document.body.clientWidth > 760` 决定（用 `body.clientWidth` 而非 `innerWidth`，因为 zoom 不改后者，见 `ui/viewport.ts` 注释）。 |
| 前端 `ui/Pane.tsx` | 同上。 | 两个 reveal 钩子改指向 `revealOrHint`：宽屏 `revealBrowser()`（原行为不变），窄屏只置 `browserHint`；`.composeaux` 内渲染 `.browserhint`（`browser.hint-open` / `browser.hint-dismiss`）。 |
| 前端 `ui/BrowserTools.tsx` | 设置里没有对应开关。 | 「启用内置浏览器」下方加「agent 打开网页时自动显示」（`browser.follows`）。 |
| 内核 `platform/browser/shell.go` `Snapshot` | ①错误页被当正文；②地址栏/标题不刷新。 | ①响应体新增 `reason`，非空即返回 `CodeNavigationFailed`；②url/title 变化时调 `s.tabsChanged()`（该调用在 `mu.Unlock()` 之后，无死锁）。 |
| 内核 `tools/builtin/browser.go` | `browser_open` 说明写「加载并返回快照」，实际是发起时刻的快照。 | 说明改为如实描述。**注意**：改工具描述会连带 `docs/TOOL_CONTRACT.md` 与 `internal/assembly/boot/testdata/golden/tool_schemas.json` 两道派生物门，必须分别用 `-update-contract` 与 `REASONIX_UPDATE_GOLDEN=1` 重新生成。 |

## 工作目录与浏览器后端

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `BrowserShellServer.start` | 用 `InetAddress.getLoopbackAddress()` 绑定。该 API 允许返回任一协议族，**安卓 libcore 返回 IPv6 `::1`**，而内核客户端硬编码 `http://127.0.0.1:<port>`（`internal/assembly/boot/browser.go`）。绑在 `::1` 的 socket 不响应拨向 `127.0.0.1` 的连接 → 端口已分配、`REASONIX_SHELL_BROWSER_PORT` 有值，却报 `connection refused`。**本机 JVM 该 API 返回 IPv4，所以桌面测不出来。** | 改用 `InetAddress.getByName("127.0.0.1")`（新增常量 `LOOPBACK_V4`），与内核同族。 |
| `MainActivity.onCreate` | `kernel.browserShellPort/Token` 在 `KernelService.start(this)` **之后**才写入；service 与 activity 都会调 `kernel.start()`（幂等，先到者真正启动）→ service 先跑时内核从未拿到端口，浏览器工具一直 suspend。 | 把端口与 token 的发布提到 `KernelService.start` 之前。 |
| `BrowserShellServer` 构造 | 只收一个 `() -> WebView?`，而 `browserView` 是**懒创建**的（首次 `showBrowserView` 才建）→ 内核的 `browser_open` 早于前端打开面板时得到 `browser view not ready`。 | 拆成 `existingView`（back/forward/reload/close 用，没有页面就无事可做）与 `ensureView`（load/read/screenshot/click/eval 用，按需创建）。 |
| `Kernel.launch` | 工作目录只由 `builder.directory()` 设定，内核从 `os.Getwd()` 自己推断根 → 手机上壳层与内核推出来的不是同一个（侧栏是共享目录，`write_roots` 是私有目录），写入被判 `workspace.write_outside_scope`。**前三版都只改壳层设的目录，没解决内核只信推断这一点。** | 新增 `--dir <workspace>`（与电脑版 `cmd/reasonix-studio-host` 的 `launchWorkspace` 同做法）**明说**工作目录；同时设 `PWD`，让内核的 `os.Getwd` 优先采用壳层选定的字符串（安卓 FUSE 挂载可能报告不同拼写）。 |
| `Kernel.rememberedLaunchDir` | 任何不可用处（不存在/不可读/不是目录）都**静默**退回私有目录 —— 这正是三轮未察觉的原因。 | 新增 `launchWorkspaceDir()`：退回时记日志，并区分 `exists` / `canRead`（「已删除」与「无权限」需要不同处置）。 |
| `internal/frontend/cli/serve_frontend.go`（内核） | `serve` **没有** `--dir`（上游 2.37 也没有），工作区根只能来自进程 cwd。 | 新增 `--dir` flag；经 `enterWorkspace()`（`session_dir.go`）**先绝对化再 chdir**，并把该绝对路径作为 boot 的 `WorkspaceRoot` 传下去，使 cwd 与 `write_roots` 是同一字符串。不可进入时报错并退回原目录。 |

## 浏览器后端：loopback 桥

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `BrowserShellServer`（新） | 无 | 在内核与叠加 WebView 之间做 loopback HTTP 服务器，10 个端点（load/close/read/screenshot/click/eval/back/forward/reload/health），对接内核 `platform/browser/shell.go`。 |
| `BrowserShellServer.handleClient` | **该服务器一开始没有任何认证**，而 `127.0.0.1` 在同机上任何应用都能连、`/eval` 能跑任意页面脚本。 | `start()` 生成 256-bit token；每个请求校验 `Authorization: Bearer`，缺/不匹配一律 401（`/health` 也不豁免）。 |
| `MainActivity.ensureBrowserView` | `onPageFinished` 无条件上报「加载成功」，而主框架失败后 WebView **仍会**触发它 → 刚报的失败被立刻清掉。 | 加 per-load `mainFrameFailed`（`onPageStarted` 清、主框架 `onReceivedError` 置），只在没失败时上报成功。 |
| `MainActivity` / `NativeBridge` | 页面需要 `window.reasonixHost` 才能走原生叠加路径。 | `injectHostBridge()` 注入 Promise 桥（`hostCall` + `__reasonixHostReply`）；`BRIDGE_VERSION` 由 `"3"` 升到 `"4"`。 |
| `Kernel.launch` | 内核需要知道服务器端口与密钥。 | 在 `browserShellPort > 0` 时投 `REASONIX_SHELL_BROWSER_PORT` 与 `REASONIX_SHELL_BROWSER_TOKEN`。 |

## 剪贴板与文件导出

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `NativeBridge.exportFile` | `MIME_TYPE` 写死 `text/plain`，与 `.md` 不符，MediaStore 追加自己的扩展名 → `报告.md.txt`。 | 新增 `mimeFor(name)` 用 `MimeTypeMap` 从扩展名推导，**查得出才写、查不出不写**。 |
| `NativeBridge.copyText` | 返回 `void`；契约改为如实返回成败。 | 改 `Boolean`，`runCatching { … }.isSuccess`。 |
| `Kernel.rememberedLaunchDir`（新） | 会话 cwd 固定为 App 私有目录，而 `write_roots` 派生自 cwd → 文件工具写不进共享工作区。 | 读 `serve-workspaces.json` 的 `launch` 作 cwd；**要求 `launch ∈ paths`**（对齐 `workspacelist.Read`），并兼容该文件的裸数组与对象两种形状。 |
| `MainActivity.onShowFileChooser` | **1.2.2 曾在这里加 `DocumentsContract.EXTRA_INITIAL_URI` 指向 `externalstorage` 的 `recent` 根 —— 该 provider 不发布这个根，华为上被回落到「下载内容」，比不加更差。** 系统默认落点本来就是「最近」。 | **改回什么都不传**（删掉 `startAtRecent`）。 |

## 页面与壳层之间的可选接口

**两处「可选方法名不匹配」，`tsc` 不会报，只有真机点下去才现：**

| 位置 | 问题 | 修法 |
| --- | --- | --- |
| `port/host.ts` `AndroidHost` | 实现叫 `hasFilesAccess()` 并返回裸 boolean，而 `HostPort` 声明的是 `filesAccess?(): { granted: boolean }`。调用方 `host().filesAccess?.()` 拿到 `undefined`，把「没有意见」当答案，**权限请求被整段跳过**。 | 改名 `filesAccess()`，返回 `{ granted }`。 |
| `port/hub.ts` `hostCapabilities()` | `shellCanPick` 只认 `electron`；内核在非 Windows 上回答 `pickFolder: false`（只有 Windows 版自带选择器）。两者相加 → 手机被判成「没有选择器」，掉进桌面用的 `setPathOpen` 兜底（要求输入 `/srv/project`）。 | 改问壳层自己：`host().folderPicker() === "native"`。 |

**为什么会漏**：`filesAccess` / `requestFilesAccess` 等都是**可选**成员（带 `?`），
TypeScript 允许一个类不实现它们，所以两处接缝都骗过了 `npx tsc --noEmit`。
**给可选成员补测试是唯一能钉住契约的手段** —— 见 `src/port/host_files.test.ts`（6 项）
与 `src/port/hub.test.ts` 里那条「手机壳下 `pickFolder` 必须为 true」。

## 权限、桥与内核

**新增的安卓权限**（`AndroidManifest.xml`）：

| 权限 | 为什么 |
| --- | --- |
| `MANAGE_EXTERNAL_STORAGE` | 「打开手机上的文件夹作为工作区」。内核是 Go 二进制，只会 `open(2)` 一个真实路径，而 SAF 只给 `content://` —— 所以只能要这个宽权限，由用户在系统设置里手动开（**该权限没有弹窗**）。 |
| `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | 息屏后系统会把后台冻结，正在跑的任务就停了。 |

`READ_EXTERNAL_STORAGE` / `WRITE_EXTERNAL_STORAGE` 带 `maxSdkVersion`，只服务于 API 33 以下。

**新增的桥接方法**（`NativeBridge.kt`，页面通过 `ReasonixNative` 调用）：

| 方法 | 作用 |
| --- | --- |
| `pickFolder(startIn)` | `OpenDocumentTree` → 真实路径。**`realPathFromTree` 必须逐段校验**：`type` 与 `rel` 来自同一个 document id，信任级别相同，只挡 `rel` 会漏掉 `..:data` 这类构造；末尾还要有「canonical 结果必须仍在 base 之内」的兜底。 |
| `setRunning(bool)` | 页面告诉壳层「现在有回合在跑」，壳层据此置 `keepScreenOn`。窗口 flag 不是页面能设的，所以这个事实要过河。 |
| `hasAllFilesAccess()` / `requestAllFilesAccess()` | 查询与跳转那个权限设置页。 |
| `batteryUnrestricted()` / `requestBatteryUnrestricted()` | 查询与申请电池优化豁免（这个有真正的弹窗）。 |

**内核侧**（`upstream/internal`）：

- `base/netdns/doh.go` —— 新增 DoH 解析器。它伪装成 `net.Conn` 给 Go 的 `net.Resolver` 驱动，
  所以**必须是流式帧**（两字节长度前缀），帧格式写错会让能用的端点看起来是坏的。
  代理要传 `netclient.ProxyFunc` 的结果进去，否则配置了代理的机器会直接出网。
- `base/netdns/netdns.go` 的 `InstallConfigured` —— 在 `boot/build.go` 里按配置装解析器。
- `base/neterr/neterr.go` —— `LooksLikeTLSCertificateFailure`：证书失败经重试后类型可能丢，
  只剩文本，所以补一层文本兜底（只匹配 Go 自己的 `x509:`/`tls:` 措辞，不匹配操作系统文案）。
- `platform/crashreport/` —— `process_binary_linux.go` 新增 `sameBinary`（读 `/proc/<pid>/cmdline`
  比对自身可执行文件），用于区分「pid 被复用」与「写入者还在」。`fatal.go` 的空文件清理
  依赖它，否则在手机上永远清不掉。
- `assembly/boot/browser.go` —— 无引擎时用 `SuspendPrefix` **真正移除** browser 工具
  （只是「不绑定」不够：`addBuiltins` 已经把它们播种进注册表了）。
- `assembly/boot/platform_note.go` —— 给模型的「你在安卓手机上」那一段。
