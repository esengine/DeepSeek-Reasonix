# 性能诊断（debugpprof）

<a href="./PERFORMANCE_DIAGNOSTICS.md">English</a>
&nbsp;·&nbsp;
<a href="./DESKTOP_CRASH_DIAGNOSTICS_RUNBOOK.zh-CN.md">桌面崩溃诊断</a>
&nbsp;·&nbsp;
<a href="./CAPABILITY_DIAGNOSTICS.zh-CN.md">能力诊断</a>

Go service 带有一个可选的诊断构建：记录 desktop catalog、sidebar、host RPC、
workspace 状态与持久 shell 路径的阶段耗时和归因计数。它由编译期构建标签控制——
正式构建链接 no-op 桩，零开销。

## 为什么用直接插桩

Windows 上的采样 CPU profile 会把样本算到阻塞在 syscall 的线程上，于是 I/O 密集的
阶段（catalog 扫描、sidebar 投影、registry 加载）会显示成它们从未消耗过的"CPU"。
因此这些阶段改为直接计时，慢 host RPC 则连同方法名一起记录，让启动水合能归因到
具体调用，而不是归因到某个采样点。

## 构建与运行

```bash
cd desktop
go build -tags debugpprof -o reasonix-desktop-debug.exe .
```

不加该标签时，`debugTickPhase` / `debugNote`（desktop）、`debugPhase` /
`debugSkipped`（sessioncatalog）、`debugRPCTiming`（hostrpc）、`debugConflict` /
`deepestConflictFrame`（workspacestate）、`debugPersistentFallback`（builtin）
全部编译为 no-op。

## 输出去向

启用标签后，`slog` 会镜像到 **stderr** 以及：

```
<desktopConfigDir>/logs/desktop-debug.log
```

该文件超过 8 MB 后轮转为 `desktop-debug.log.1`。桌面启动器不捕获 service 的
stderr，所以这个文件是本地运行耗时唯一的持久落点。

## 信号

| 信号 | 日志行 | 覆盖范围 |
| --- | --- | --- |
| 阶段耗时 | `debugpprof: tick phase phase=… took=…` | catalog 与 sidebar 各阶段（见下） |
| 慢 host RPC | `debugpprof: slow rpc method=… took=…` | 每一次耗时 ≥ 50 ms 的 `Registry.Invoke` |
| 变更冲突 | `debugpprof: workspace mutation conflict site=… …` | 11 个 raise 点，外加守卫栈帧 |
| catalog 扫描 | `debugpprof: catalog phase phase=signature\|scan target=… took=…` | 每次目录 reconcile |
| catalog 跳过 | `debugpprof: catalog scan skipped (signature unchanged) target=…` | 签名未变的目录 |
| shell 回退 | `debugpprof: persistent shell fallback shell=… cause=…` | 降级为隔离执行 |

### 阶段耗时

| 分组 | 阶段名 |
| --- | --- |
| 快照 RPC | `snapshot-rpc`、`snapshot:projects-file`、`snapshot:project-shells` |
| 主题列表 | `list-topics:availability:<root>`、`list-topics:catalog-page:<root>`、`list-topics:merge-metadata:<root>`、`list-topics:live:<root>` |
| catalog 分页 | `catalog-page:preferred:<root>`、`catalog-page:list-topics:<root>` |
| sidebar legacy | `legacy:<root>`、`merge-shells`、`legacy-page:<root>` |

### workspace 变更冲突

`debugConflict` 记录一次"持久化状态 vs 期望值"的分歧，以及触发它的站点。
`deepestConflictFrame` 会追加最内层的 `lifecycle.go` / `store.go` 栈帧，形如
`raise=<file>:<line>`——于是 `commitOperation` 或 `PrepareOperationContent` 内部的
守卫会被归因到那个守卫本身，而不是归因到调用 `mutate` 的 `Store` 方法。

## 实时 profile

在启用标签的同时设置 `REASONIX_PPROF=1`，即可在 `127.0.0.1:6060` 暴露
`net/http/pprof`（CPU、heap、goroutine、block、mutex profile）。

```bash
REASONIX_PPROF=1 ./reasonix-desktop-debug.exe
```

## 覆盖范围

只覆盖 Go service 侧。Electron/渲染侧由
`.github/workflows/diagnostic-overhead.yml` 单独覆盖；上面这些 Go service 阶段
正是该构建标签所填补的空白。

## 测试

该标签构建此前没有任何测试。现在补上了：

- `desktop/debug_diagnostics_test.go` —— 正式构建所链接的 no-op 契约（`//go:build !debugpprof`）。
- `desktop/debug_diagnostics_debug_test.go` —— 引用标签版 `debugLogPath` 的编译门禁（`//go:build debugpprof`）。
- `desktop/internal/workspacestate/debug_ws_frame_test.go` —— `deepestConflictFrame` 不 panic，且各段为 `file:line`（无标签）。

CI 除了默认构建，也必须构建标签路径：

```bash
go build ./... && go build -tags debugpprof ./...
cd desktop && go build ./... && go build -tags debugpprof ./...
```
