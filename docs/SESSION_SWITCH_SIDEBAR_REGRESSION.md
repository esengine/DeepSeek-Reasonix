# Session switching and sidebar identity / 会话切换与侧栏身份回归

## Reproduced mechanisms / 已复现机制

Canonical `OpenSession` emits `runtime:rebuilt` and `agent:ready` before returning.
The passive readiness handler and explicit navigation both hydrated the target,
while rebuild notifications could restart its follower. A gated host fixture
reproduced three baseline calls for one selection, including reads before the
target binding committed. The navigation intent now owns hydration through its
completion; stale completion cannot release a newer intent. Repeated notices
for the same runtime epoch preserve its follower, while an actual replacement
still creates a new subscription. Prompt bookkeeping still resets on rebuild.

规范会话的打开接口会在返回前发送重建和就绪事件。被动就绪处理与主动打开流程
都加载目标历史，重建通知还可能重启订阅。使用可控异步屏障，修复前一次选择
触发了三次 baseline 读取，其中包含绑定提交前的读取。现在由当前导航意图统一
负责加载；旧操作结束不能释放新操作的归属。同一运行时的重复通知保留订阅，
真正替换运行时仍建立新订阅，审批等重建状态继续正常重置。

Reviewed versions of a legacy source inherited its unversioned path alias.
Separately, older source spellings could normalize to one alias while belonging
to different durable sessions. The renderer's mounted-key cache allowed those
canonical sessions to share a React key. Reordering a three-row fixture left
four DOM rows before the fix, without creating any session data.

旧来源的独立审核版本错误地继承了无版本路径别名；旧路径拼写归一化也可能让
独立会话共享来源别名。前端挂载键缓存允许两个规范会话共用 React key。
修复前，三条数据在重排后留下四条 DOM 行，无需新建任何会话即可复现增长。

The source owner now publishes only version-specific aliases for reviewed
sessions and removes ambiguous cross-owner aliases. Exact durable receipts
remain independently addressable. The renderer also refuses to transfer an
already owned mounted key to another canonical SessionRef, including rows
already loaded from older hosts. Unambiguous source adoption keeps its row key.

修复后，审核版本只使用其版本身份，多个会话争用的别名不会作为唯一身份发布，
原有持久化回执仍可分别寻址。前端不再把已归属一个规范会话的挂载键交给另一个
会话，兼容旧宿主已经返回的行；无歧义的来源迁移仍保留原挂载键。

## Regression checks / 回归验证

Each bounded history read also opened its locator database for both freshness
validation and paging. Both opens ran a whole-database integrity audit. History
locators now opt into bounded, process-local audit certificates: a successful
full check can be reused only for the same file identity, size, mtime and SQLite
header, with no nonempty WAL. Replacement, writes, WAL data, failed audits and
mutation during opening/auditing invalidate reuse. No SQLite handles are retained;
other stores retain their existing default full audits and rebuild validation.

有界历史读取还会为新鲜度校验和分页分别打开定位索引，每次都执行整库完整性
校验。历史索引现在显式启用有界、进程内的校验凭据：仅当文件身份、大小、修改
时间和 SQLite 文件头一致，且没有非空 WAL 时，才复用此前成功的完整校验。
替换、写入、WAL 数据、校验失败及打开/校验期间的变化均使复用失效。不保留
SQLite 连接或文件句柄；其他存储默认的完整校验及重建校验保持原有行为。

- `canonical-navigation-hydration.test.tsx`: publication barriers, one baseline,
  late duplicate notices, genuine runtime replacement, superseded navigation,
  and failed-open cleanup. / 验证提交屏障、单次读取、迟到通知、真实替换、
  快速切换及失败后的归属释放。
- `project-session-row-identity.test.tsx`: source adoption followed by repeated
  sibling reorder/insertion/removal with conflicting legacy aliases, without
  DOM growth or React warnings. / 验证来源迁移及冲突别名下反复重排、插入、删除。
- `TestWorkspaceSourceAliasesKeepReviewedSessionsIndependent` and
  `TestWorkspaceSourceAliasesRejectAmbiguousPathOwnership`: backend alias
  ownership with unchanged independent receipts. / 验证后端别名归属和独立回执。
- `TestIntegrityReuse*`: unchanged files, nonempty WAL, replacement with equal
  size/mtime, SQLite header changes, failed audits, concurrent mutation, quick
  versus full audit separation, real constraint corruption, concurrent readers
  and bounded certificates. `BenchmarkHistoryProjectionRepeatedOpen` compares
  the old full-audit path with reuse over a roughly 100 MiB SQLite fixture.
  / 覆盖文件不变、非空 WAL、等大小/时间替换、文件头变化、失败及并发变化、
  快速/完整校验隔离、真实约束损坏、并发读取和凭据容量；另以约 100 MiB 的
  SQLite 夹具对比完整重复校验和复用校验的开销。
- `node bench/session-switch-sidebar.mjs`: real Chromium, 50 catalog refreshes,
  five opens, no premature reads, one target baseline per open, clean console.
  The fixture is included in `pnpm test:session-loading-browser`.
  / Chromium 验证 50 次列表刷新与五次打开，绑定前零读取，每次打开一次 baseline，
  控制台无警告；已纳入会话加载浏览器测试。

Existing transcript, tab switching, new-session races, topic activation,
pending-prompt, app-lifecycle, historical-source/version, sidebar browser,
typecheck and production build gates cover adjacent contracts. Desktop Go
alias/source regressions are also run with the race detector.

现有 transcript、标签切换、新建会话竞态、话题激活、审批、应用生命周期、
历史来源及版本、侧栏浏览器、类型检查和生产构建用于覆盖相邻行为；Desktop Go
别名及来源测试同时执行竞态检测。

Local measurement on a 110,911,488-byte SQLite fixture (ten measured opens):
full audits averaged 44.15 ms/open, reused audits 3.42 ms/open. The same-toolchain
isolated frontend baseline measured 2,107,855 initial raw bytes; the fix measured
2,108,229 (+374 bytes, 0.018%). The raw ceiling is 2059.0 KiB, retaining the
existing 186-byte producer margin; compressed, CSS and chunk budgets are unchanged.

本地 110,911,488 字节 SQLite 夹具的十次读取均值：完整重复校验为 44.15 ms，
复用校验为 3.42 ms。同工具链隔离构建的首屏未压缩基线为 2,107,855 字节，
修复后为 2,108,229 字节（增加 374 字节，0.018%）。未压缩阈值为 2059.0 KiB，
保留原有的 186 字节构建环境余量；压缩体积、CSS 和分块预算未放宽。

Local acceptance / 本地验收：

| Gate / 检查 | Result / 结果 |
| --- | --- |
| Root `go test -p 4 ./...` | Passed, 158 tested packages / 通过，158 个有测试的包 |
| Projection/session race suites and Desktop targeted race tests | Passed / 通过 |
| Frontend transcript, navigation, prompt and app lifecycle tests | Passed / 通过 |
| Production build, source/test typechecks and bundle gates | Passed / 通过 |
| Chromium sidebar and session-loading suites | Passed / 通过 |
| Repository lint and affected Go/desktop lint | Passed / 通过 |
| Windows amd64 Desktop cross-build | Passed; not a Windows runtime test / 通过；不代表 Windows 实机验证 |

## Evidence boundaries / 证据边界

These are synthetic local regressions, not a replay of the user's private
session or a Windows performance measurement. Browser fixture elapsed times
include controlled publication and input actions; do not report them as a
production speedup. `recovered.duration_ms` also includes the first successful
delta poll after baseline installation, which may wait 25 seconds when idle.
Global `readDiagnostics` counters are not a per-session timing trace.

测试使用本地构造数据，不是用户私有会话的回放，也不是 Windows 性能实测。
浏览器夹具耗时包含受控提交及输入操作，不能当作生产环境提速数据。
`recovered.duration_ms` 包含 baseline 安装后的首次成功增量轮询，空闲时可能
额外等待 25 秒；全局 `readDiagnostics` 也不能当作单会话的阶段耗时。

No persisted format, conversation content, or provider-visible prompt changes
are required. The follow-up below adds an optional Desktop event field and a
local read-only RPC. / 本修复无需修改存储格式、会话正文或模型可见的提示词；
下述补充修复新增一个可选 Desktop 事件字段及本机只读 RPC。

## Follow-up on latest main-v2 / 最新主线上的同类修复

Baseline: `origin/main-v2` at `8cc804e687` (2026-10-24). All original fixes
were rebased onto that commit before the following changes.
基线为 2026-10-24 的 `origin/main-v2`，提交 `8cc804e687`；原有修复先变基，
再补齐以下同类问题。

### Canonical aliases / 明确会话身份与别名

Page merging, incomplete pages, pinned rows and runtime overlays now share a
resolver that gives host/session identity precedence. Only a unique owner can
adopt a legacy source; conflicting canonical rows remain independent. An
ambiguous source retained alongside canonical versions receives a separate
mounted key. Unidentified live tabs still adopt their own placeholders.
分页、不完整目录页、置顶行及运行状态叠加统一使用身份解析器，优先采用
host/session 身份；仅唯一归属的旧来源可以合并。存在歧义的来源与独立历史版本
保持区分，挂载键互不冲突；尚未取得 SessionID 的活跃标签页仍能接管自己的占位行。

Archive fences retain each canonical owner independently, including source
archive receipts naming multiple adopted targets. Another session's alias or
larger lifecycle generation cannot hide a sibling or release its archive.
归档屏障按明确会话分别保存，来源归档回执包含多个已迁移目标时亦如此。
其他会话共享别名或更高生命周期版本，不会误隐藏同级会话或解除其归档屏障。

`session-alias-isolation.test.ts` failed all four initial cases before the fix;
it also covers multi-target source receipts. Existing runtime projection tests
protect pathless live-tab adoption. React and Chromium cover repeated reorder,
pagination, archive, unrelated sibling publication, and exact restoration.
新增身份测试在修复前四项均失败，并补充多目标来源回执测试；既有运行状态测试
保护无路径标签页迁移。React 和 Chromium 覆盖反复重排、分页、归档、同级会话
更新及准确恢复。

### Derived read indexes / 派生读取索引

All history and search opens now go through `openHistoryProjection` or
`openSearchProjection`, including body ranges, attachment authorization,
exports, freshness checks and incremental updates. Rebuilds still validate
their newly populated database; unrelated authoritative stores keep full checks.
所有历史及搜索索引打开统一经过上述入口，覆盖正文分块、附件授权、导出、
新鲜度检查和增量更新。重建仍校验新生成的数据库，其他权威存储仍执行完整校验。

`TestSessionReadPathsReuseIntegrityAndRecheckChanges` uses SQLite CHECK-expression
probes to count actual integrity audits through query APIs, without timing
thresholds. Before repair, three unchanged attachment lookups performed three
extra audits; export and search performed six each. After repair, unchanged
reads perform zero extra audits, and external writes require a new audit.
该测试利用 SQLite CHECK 表达式观测实际完整性校验次数，不依赖机器耗时阈值。
修复前三次附件查询额外校验三次，导出和搜索各六次；修复后未变化的读取不再
额外校验，外部写入仍强制重新校验。正文分块及历史窗口一并覆盖。

### Remote hydration / 远程历史加载

The real remote hook reproduced an extra baseline when the same ready barrier
arrived during mounting. State publications now carry a monotonic per-tab
`hydrationRevision`; `RemoteTabHydrationState` reads the same publication under
the route lock, without contacting Serve. A frontend owner joins in-flight or
installed baselines for that revision, rejects old barriers, and permits retries
after failure. New ready-to-ready revisions still reload after reset, resume or
ownership transfer. Disconnect invalidates pending completion. Explicit user
retry remains available.
真实远程 Hook 已复现：挂载期间到达相同 ready 通知会额外读取一次基线。
状态通知现在携带标签页内递增的 `hydrationRevision`；新增只读 RPC 在路由发布锁内
读取同一状态，不访问远端 Serve。前端按版本复用正在加载或已安装的基线，拒绝旧
通知，失败后可重试。真实重置、恢复或归属切换产生的新版本仍会重新加载；断线
使未完成的旧响应失效，用户显式重试功能保留。

| Contract / 契约 | Compatibility / 兼容行为 |
| --- | --- |
| Optional event revision / 可选通知版本 | Old renderers ignore it; missing revisions preserve legacy ready invalidations. / 旧前端忽略新增字段，缺少版本时保留原 ready 重载语义。 |
| Read-only mount snapshot / 只读挂载快照 | An unavailable command falls back to existing hydration. / 旧宿主缺少该命令时回退到原加载流程。 |
| Session/provider data / 会话与模型数据 | No persisted or provider-visible bytes change. / 不修改持久化格式或模型可见内容。 |

Deterministic hook, owner and Go publication tests cover duplicate notifications,
genuine resets, supersession, failure retry, disconnect, legacy notifications and
atomic mount snapshots. Chromium verifies duplicate ready, newer resets and an
older baseline completing after the latest one. The new regressions run through
`pnpm test:session-switch-regression`, also required by the session-loading browser suite.
确定性 Hook、归属管理器及 Go 发布测试覆盖重复通知、真实重置、快速替换、失败重试、
断线、旧通知兼容及挂载状态原子读取；Chromium 验证重复 ready、新版本重置和旧基线
晚到。新增回归统一纳入上述命令，并接入会话加载浏览器测试。

The complete fix measures 2,109,361 initial raw bytes versus 2,107,862 on isolated
latest main-v2 with the same toolchain (+1,499 bytes, 0.071%). The raw ceiling is
2060.2 KiB, retaining the established 186-byte producer margin. All compressed,
CSS, locale, chunk and history residency budgets are unchanged.
同工具链隔离构建最新主线为 2,107,862 首屏未压缩字节，完整修复为 2,109,361 字节
（增加 1,499 字节，0.071%）。未压缩阈值为 2060.2 KiB，保留既有 186 字节生产构建
余量；压缩体积、CSS、语言包、分块及历史驻留预算均保持原有要求。

### Final local acceptance / 最终本地验收

Verified on macOS, 2026-10-25 / 于 2026-10-25 在 macOS 本地验证：

| Gate / 检查 | Result / 结果 |
| --- | --- |
| Root `go test -p 4 ./...` | 158 packages passed / 158 个包通过 |
| Desktop CI partition inventory and all 12 groups | All passed; 3,249 test/example/fuzz entry points assigned / 全部通过，清单覆盖 3,249 个测试、示例及模糊测试入口 |
| Projection/history and Desktop navigation/publication race tests | Passed / 通过 |
| Frontend typechecks, transcript, remote, app lifecycle, 18 sidebar test files | Passed / 通过 |
| Session-loading Chromium suite and final local/remote regression fixture | Passed / 通过 |
| Production build and bundle budgets; both Go modules' lint; repolint; generated contract/inventory | Passed / 通过 |
| Windows amd64 Desktop cross-build | Passed; not a Windows execution / 编译通过，非 Windows 实机运行 |

Desktop broad validation uses the existing `desktop-windows-go-tests.mjs`
partitions, whose inventory check proves complete coverage. Its own TestMain
and per-test helpers isolate homes; leave inherited `REASONIX_HOME` unset.
The terminal-state notification regression now parses and checks state, error,
publication revision and exactly one notification instead of rejecting the
additive revision field through an obsolete full-JSON string comparison. The
affected Q-S partition was rerun successfully after that contract update.
Desktop 全量验证采用既有分组工具及完整性清单。测试入口和逐测试辅助函数自行
隔离目录，应清除继承的 `REASONIX_HOME` 覆盖。终止状态通知回归现解析并检查状态、
错误、发布版本和恰好一次通知，替代会拒绝新增可选字段的旧完整 JSON 字符串比较；
更新契约后，受影响的 Q-S 分组已完整重跑通过。
