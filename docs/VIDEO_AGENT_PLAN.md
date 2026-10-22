---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-20
---

# Video-capable agent: implementation handoff / 视频制作能力开发交接

## Objective / 目标

Improve Reasonix's ability to make, inspect, revise, resume, and deliver
videos through its available tools. The architecture is model-independent.
DeepSeek Flash is a preferred efficiency target, not a dependency or a
mandatory planner.

Choose execution and review paths from declared capabilities of the current
model and installed tools. Never infer vision, audio, or video support from a
model name. A text/tool model can orchestrate a video workflow without itself
being a video generator or audiovisual reviewer.

目标是提升 **Reasonix agent 本身制作视频的能力**：制作、检查、修改、中断恢复和交付。 架构不绑定模型；DeepSeek Flash
是优先优化和评测的选择，并非必要条件。 按当前模型和已安装工具声明的能力分配工作，不从模型名称猜测视觉、音频或视频能力。

文本模型可以编排视频任务；真正的生成、剪辑和验收由具备相应能力的工具或模型完成。

This is a draft handoff, not a release claim. Keep the PR in draft until another
agent completes the remaining stages and the acceptance gates below. Do not
publish media, install paid services, or change the user's chosen model as a
side effect of implementing the harness.

本 PR 保持草稿，供其他 agent 继续开发。完成下面的实现与验收条件后才能另行考虑转为正式评审。
实现执行框架本身不授权发布成片、接入付费服务或替换用户所选模型。

## Current checkpoint / 当前进度

| Area / 范围 | State / 状态 | Evidence or entry point / 证据与入口 |
| --- | --- | --- |
| Full MCP result retention / 完整结果保留 | Implemented / 已实现 | `internal/ext/plugin/toolresult.go`, `internal/tools/jobs/tool_result.go` |
| Session-scoped result readback / 会话范围结果读取 | Implemented / 已实现 | `internal/contract/tool/result.go`, `internal/tools/builtin/readfile_result.go` |
| Accepted MCP Tasks restart recovery / 已接收 MCP 任务重启接管 | Implemented / 已实现 | `internal/ext/plugin/task_recovery.go`, `internal/tools/jobs/remote_task.go`, `internal/assembly/boot/remote_task.go` |
| Provenance-backed download / 带来源下载 | Conditional tool, wired when an MCP server is configured / 条件工具，配置 MCP 服务时挂载 | `internal/tools/builtin/artifact_{source,download,transfer}.go` |
| Download tests / 下载测试 | Focused tests implemented / 已有定向测试 | `internal/tools/builtin/artifact_download_test.go` |
| Technical video acceptance / 成片技术验收 | Conditional tool, wired when ffmpeg is installed / 条件工具，安装 ffmpeg 时挂载 | `internal/tools/builtin/checkvideo{,_probe}.go` |
| Storyboard/content/audiovisual acceptance / 分镜内容与视听验收 | Manifest review records; `unverified` without a declared reviewer / 清单验收记录，无声明审查者时记为未验证 | [VIDEO_MANIFEST.md](VIDEO_MANIFEST.md) |
| Video production skill and manifest / 制作技能与清单 | Inline builtin skill + manifest v1 spec / 内置技能与清单 v1 | `internal/ext/skill/builtincontent/video-production/SKILL.md`, [VIDEO_MANIFEST.md](VIDEO_MANIFEST.md) |
| Unified preview/download/version UI / 统一产物界面 | Workbench video player and manifest view over `/workspace/video` / 工作台视频播放与清单视图 | `internal/frontend/serve/workspace_video.go`, `desktop/frontend-next/src/ui/WorkspaceMedia.tsx` |
| Typed scripts, bounded concurrency, host waits / 类型化脚本、并发、等待 | `try_call` returns `result_ref`; `call_many` batches through the host scheduler; service adapters open / 已实现，服务适配待定 | `internal/tools/script/batch.go`, `internal/runtime/agent/nested_invoke.go` |
| Live video-service and model evaluation / 真实服务与模型评测 | Offline suite and fake service ready; no model run yet / 离线评测集与模拟服务就绪，尚未运行模型 | [benchmarks/video](../benchmarks/video/README.md), `cmd/fakevideomcp` |

The foundational commit is `12d6f3cf18`, based on `598de997fa`. It passed the
root Go suite (205 packages), Desktop Go suite, lint, vet, and focused races at
that checkpoint. This is historical evidence, not a claim that the download
prototype or the latest upstream integration passed the same gates.

底层能力提交为 `12d6f3cf18`，基于 `598de997fa`；该检查点通过根模块 205 个包、Desktop Go、
lint、vet 和定向 race 测试。这是历史验证，不代表下载原型或最新上游整合已经通过相同验证。
交接时上游 `studio` 已明显前进；先检查 PR 的实时基线、冲突和差异，再做集成。

A non-mutating trial merge against `studio` at `0b8de9e636` found a content
conflict in `internal/session/control/controller.go`. No upstream changes have
been merged into this checkpoint. Resolve lifecycle/dependency ownership against
the current controller before evaluating the integrated branch.

与 `0b8de9e636` 的试合并发现 `internal/session/control/controller.go` 内容冲突；尚未将
上游整合到本检查点。接手后先对齐当前控制器生命周期和依赖所有权，再验证整合分支。

The checker test draft now runs as `internal/tools/builtin/checkvideo_test.go`
with every original requirement intact.

验收测试草稿已迁为 `internal/tools/builtin/checkvideo_test.go` 并执行，原有行为要求均保留。

`download_artifact` registration:

- Declared with `tool.RegisterConditional`; `addMediaTools`
  (`internal/assembly/boot/media_tools.go`) attaches it only when an MCP server
  is configured, since only MCP results produce `tool-result:` references.
- Outside the 7,185-token built-in total and the provider prefix; the model
  reaches it through `use_capability`.
- `TestConditionalMediaSchemasStayCompact` caps its schema. No budget was widened.

`download_artifact` 注册方式：

- 以 `tool.RegisterConditional` 声明，仅在配置了 MCP 服务时由 `addMediaTools` 挂载。
- 不计入 7,185 内置预算，不进入 provider 前缀，模型经 `use_capability` 调用。
- 其 schema 由单工具上限测试约束；未放宽任何预算。

## Architecture and invariants / 架构与约束

1. The canonical result is retained separately from model/UI projections. The
   model receives short receipts and durable references; rich media and full
   JSON do not repeatedly inflate the conversation.
2. The shared runtime/controller owns execution, permissions, cancellation,
   recovery, and provenance. Frontends render the same facts. A skill describes
   creative work; it does not replace the host's execution guarantees.
3. Three identities stay distinct: host job ID, remote service task ID, and
   immutable artifact/version ID. A submission, successful generation,
   completed download, and accepted delivery are different events.
4. Evidence binds to artifact SHA-256, requirements revision, and reviewed
   time range. Replacing the bytes or requirements invalidates affected review
   evidence. A sampled frame does not prove motion, audio, or synchronization.
5. User-owned files and unknown persisted fields survive upgrades. Unknown
   format versions are retained and refused for mutation; no lossy rewriting.
6. Tool definitions and system prefix remain stable within the session. Dynamic
   tasks, manifests, reports, and capability results travel in the turn tail.

规范结果与模型、界面视图分离；共享运行时拥有权限、取消、接管和来源，前端只呈现同一事实。 宿主 job、远端
task、产物版本分别标识。生成完成不等于下载完成，下载完成不等于验收通过。 验收证据绑定文件摘要、需求版本和检查范围；修改文件或需求后重新检查受影响部分。

旧数据和未知字段保留；动态状态放在回合尾部，不能反复改写系统提示词或工具定义。

## Stage 1 — Finish artifact download / 第一阶段：完成产物下载

### Existing prototype contract / 已有原型契约

`download_artifact` takes a session `tool-result:` reference, a JSON pointer
relative to its retained `result`, a new workspace-relative `path`, and optional
`max_bytes` (default and ceiling: 4 GiB). Examples:

```json
{
  "source": "tool-result:<digest>",
  "pointer": "/structuredContent/video_url",
  "path": "video-project/assets/shot-02-v1.mp4",
  "max_bytes": 268435456
}
```

It also supports an explicit resource pointer such as `/content/1/uri`; it does
not scrape prose or guess field names. It reads the version-1 result envelope
through the session reader, rejects error results and missing fields, and does
not grant the URL the authority of an address written by the user.

原型通过会话结果引用和明确 JSON 指针选择地址，也可选择资源链接的 `uri`；不会解析自然语言或
猜测 `job_id`、URL 字段。只读取支持版本的本会话结果，拒绝失败结果和缺失字段，不把服务返回的
地址冒充用户输入过的地址，因此不放宽现有 `read_video(url=...)` 的授权边界。

The implementation streams into a private staging file, bounds size and time,
recognizes container magic bytes, computes SHA-256, and publishes without
replacing an existing target.

Publication and receipt:

- The media is published create-only first, then a versioned
  `<path>.source.json` receipt, so a receipt always names bytes that exist.
- The receipt records source reference, pointer, origin (scheme and host),
  redirect hosts, byte count, hash, and completion time.
- It never holds a query, fragment, or userinfo; the exact signed address
  stays in the private `.tool-result` sidecar the reference names.
- Redirects may cross origins: at most 5, every hop through the SSRF dial
  guard, no HTTPS downgrade, credentials dropped on a change of origin.
- Model output contains relative paths, byte count, and hash only.

POSIX receipt mode is `0600`; Windows permission behavior needs its own
acceptance rather than a POSIX-mode assertion.

原型流式下载到临时文件，限制大小和耗时，识别容器头、计算摘要，并拒绝覆盖目标。
- 先只新建发布媒体，再写来源旁文件，有回执就一定有文件。
- 回执记录结果引用、指针、来源 scheme+host、重定向主机、大小、摘要和时间。
- 回执不含 query、fragment、userinfo；精确签名地址只留在私有 `.tool-result` 中。
- 重定向允许跨源：最多 5 跳、每跳 SSRF 拨号校验、禁止 HTTPS 降级、换源丢弃凭据。
- 模型仅收到精简回执。
POSIX 使用 0600，Windows 权限需要单独验证。容器头识别仅用于下载准入，不代表完整解码成功。

### Remaining implementation / 待完成实现

- Review networking at the actual transport boundary, including redirects,
  rebinding, cancellation, proxy configuration, and signed-URL redaction. The
  prototype reuses `web_fetch` policy: local loopback is allowed and a
  configured proxy is a trusted resolver.

  Do not describe it as a blanket public-address-only guarantee. Any policy
  change belongs in the shared owner.
- Crash states are not download success:
  - between linking and the receipt, media exists without provenance;
  - during transfer, a `.reasonix-artifact-*` staging file stays beside the target.

  `check_video` with the expected hash establishes success. Without hard
  links the media is copied into a create-only target; nothing overwrites.
- Add explicit generic artifact identities and relationships to host job,
  remote task, shot, and version where needed. Obtain identities from declared
  contracts, not field-name or message-text inference.
- Keep schemas deterministic and expose the capability through the existing
  discovery path. Verify it reaches the real runtime, including path grants,
  write receipts, external-data labeling, and result-reader context.

待补网络策略实测、崩溃后临时文件与来源记录核对、磁盘满与跨平台发布行为；仅有来源文件不能算
下载成功。还需解决签名地址在工作区旁文件中的生命周期与同步泄露风险，优先采用脱敏可携带来源
加会话私有记录。产物和任务、镜头版本的关系应由明确契约给出，保持现有权限和发现通道。

### Acceptance / 验收

Test exact structured/resource pointers, escaped pointer keys, foreign
sessions, unknown envelope versions, failed MCP results,
credentials/fragments, private targets, redirect policies, chunked overflow,
truncated bodies, cancellation, disk errors, symlink escapes, preexisting
media/receipts, and restart cleanup.

Use `boot.Build` with a disposable MCP/HTTP service to prove the full result →
download → confined file → receipt path, without paid services. Compare actual
provider system/tool-prefix bytes before and after the workflow.

用例覆盖来源、会话边界、版本、网络、流量限制、取消、磁盘、符号链接、已有文件及崩溃恢复。
通过真实 `boot.Build` 加本地模拟服务证明完整链路，并比较真实 provider 请求前缀。

## Stage 2 — Evidence-based video acceptance / 第二阶段：有证据的成片验收

### Technical tool / 技术检查工具

`check_video` (`internal/tools/builtin/checkvideo.go`):

- wraps the workspace's `read_video` binding, sharing its confinement,
  container recognition, media-process launching, and sandbox;
- is attached by `addMediaTools` only when ffmpeg and ffprobe are on `PATH`;
- asks before its first use where no OS sandbox wraps the decoder
  (`media_approval.go`).

Inputs should name a local file and explicit expected duration with tolerance,
dimensions, display aspect ratio, and audio presence policy. An optional
expected hash should detect replacement since download.

实现 `check_video`，复用现有读取范围、容器识别、媒体进程启动和沙箱约束；输入为本地文件和明确
验收规格，包含时长容差、分辨率、显示比例、音轨要求，可指定下载时的摘要以发现文件替换。

Use an immutable, bounded snapshot so probing, decoding, and hashing describe
the same bytes. Own and clean up its temporary directory, attach it to the
actual OS sandbox, and use the session temporary-directory lifetime where
available.

Do not open playlists or arbitrary demuxers, or let a media file fetch remote
data/external file references. Keep output bounded and cancellation effective.
In unsandboxed environments apply the same media approval contract as
`read_video`, respecting explicit user rules.

检查快照必须固定，探测、解码、摘要都对应同一份字节；临时目录进入实际沙箱并可靠清理。
拒绝播放列表、任意 demuxer、外部文件引用与媒体内部联网，限制输出并响应取消。
无系统沙箱时沿用媒体工具审批规则，尊重用户已有显式规则。

Run a full video/audio decode, not a thumbnail sample. Handle probe errors,
missing streams, invalid/unknown durations, non-square pixels, display
rotation, audio policy, late corruption, and decode timeout explicitly.

Return bounded JSON with version, artifact hash/bytes, observed facts,
requested checks, technical pass/fail, failure codes, and unchecked scopes. A
decode timeout is incomplete evidence, not a pass. The draft tests are a
starting point, not the whole test matrix.

执行完整视频与音频解码，不能以抽帧替代。明确处理缺轨、未知时长、非方形像素、旋转、音轨策略、
尾段损坏和超时。JSON 报告记录版本、摘要、事实、检查项、技术结论、失败代码和未检查范围；
解码超时是证据不完整，不是通过。现有测试草稿只是起点。

### Content and audiovisual review / 内容与视听检查

- Build review requests from the storyboard's explicit time ranges and
  acceptance criteria. Use frame review for visible subjects, layout, captions,
  and scene continuity only within its actual sampling coverage.
- Discover a capable native-video/audio tool or model for motion, speech,
  music, and synchronization requirements. Preserve the selected model unless
  the user's configuration or explicit routing permits another reviewer.
- If no suitable reviewer exists, record `unverified` with the missing
  capability. Do not fail the entire workflow merely because Flash is selected,
  and do not claim that Flash or still frames checked unsupported modalities.
- Persist scope, reviewer/tool identity, artifact hash, requirement revision,
  reviewed ranges, observations, verdict, and evidence reference. Distinguish
  `pass`, `fail`, `unverified`, and explicitly `not_applicable`.
- Mark an artifact accepted only when every required scope has valid evidence.
  A provisional delivery must state remaining checks and obtain the user's
  acceptance of that reduced scope. Rework only the failed shots or assembly
  stages within the agreed cost/retry budget.

按分镜时间点和验收条件构造检查；抽帧仅支持实际覆盖的画面事实。运动、音频、同步需要对应能力。 能力缺失时标为未验证，不因选择 Flash
阻断整个制作流程，也不能假称它已检查不支持的模态。 记录检查者、工具、摘要、需求版本、时间范围、结论和证据；必需项全部有证据才可验收通过。

返工只重做失败镜头或受影响的合成阶段，遵守预算；暂交版本明确缺项并取得用户对缩减范围的接受。

### Acceptance / 验收

Use generated local fixtures plus deterministic process fakes: valid video,
silent video, required/forbidden audio, wrong duration/resolution/ratio,
rotation/SAR, corruption near the end, missing ffmpeg, forbidden paths,
changed source, timeout/cancel, snapshot cleanup, and error output bounds.

Include a real ffmpeg integration where available and a `boot.Build` effect
proving the report reaches the model without prefix churn. Tests must ensure
missing semantic or audiovisual evidence cannot become final acceptance.

用本地生成素材和确定性模拟进程覆盖技术检查矩阵，并验证报告进入模型的真实边界。
缺失内容或视听证据时不能自动变成最终通过；真实 ffmpeg 验证与可重复的单元测试互补。

## Stage 3 — Production skill and persistent manifest / 第三阶段：制作技能与持久清单

Ship an inline `video-production` skill through the existing embedded-skill
mechanism (`internal/ext/skill/builtincontent`). Keep it self-contained unless
the embed mechanism also makes references accessible. Project/user overrides
and skill discovery must continue to work.

The body is loaded on invocation; the full creative workflow must not become a
permanent system prompt.

沿用内置技能机制交付 `video-production`，内联执行；保留用户、项目覆盖和发现机制。
除非引用资源能实际读取，否则采用自包含正文。技能按需加载，不把整个制作流程塞进常驻系统提示词。

Use this workflow: brief → script → storyboard → assets → generation jobs →
assembly → acceptance → delivery. Discover generation/editing/review tools
before promising execution. Save a versioned workspace manifest before paid or
long-running submission and after each meaningful transition.

Prefer existing file read/edit/write paths for the first skill; do not
introduce a second task engine inside the skill. If automatic host updates are
added later, their owner must enforce revision checks and lossless unknown-
field round trips.

工作流为需求、脚本、分镜、素材、生成任务、剪辑、验收、交付。先发现可用工具再承诺执行；
付费或长耗时提交前、每次关键状态变化后保存项目清单。初版优先复用文件工具，技能不另建任务引擎。
后续若由宿主自动更新清单，由状态拥有者保证版本比较和未知字段无损往返。

Proposed manifest fields (version 1; additive and preserved on edits):

| Group / 分组 | Required facts / 必需事实 |
| --- | --- |
| Identity / 身份 | `version`, `production_id`, `revision`, brief/script revision / 格式版本、制作 ID、修订号、需求和脚本版本 |
| Requirements / 要求 | Duration, aspect ratio, resolution, audio/subtitles, required review scopes, output format, budget / 时长、比例、分辨率、音轨字幕、验收范围、格式、预算 |
| Shots / 镜头 | Stable `shot_id`, timeline, prompt revision, reference asset IDs, dependencies / 稳定镜头 ID、时间轴、提示词版本、参考素材、依赖 |
| Jobs / 任务 | Service/tool identity, host job, remote task, idempotency key when supported, submission outcome / 服务工具、宿主和远端 ID、可用幂等键、提交结果 |
| Artifacts / 产物 | Stable ID and version, local path, source receipt/ref, SHA-256, bytes, superseded version / ID 与版本、路径、来源、摘要、大小、历史关系 |
| Reviews / 验收 | Requirement revision, artifact hash, scope/ranges, reviewer, verdict, evidence reference / 需求版本、文件摘要、检查范围、检查者、结论、证据 |
| Delivery / 交付 | Final artifact version, required-check coverage, limitations, user acceptance if provisional / 最终版本、必需检查覆盖、限制、暂交接受记录 |

Keep remote execution state separate from production stage. The manifest can
describe `planned`, `submission_unknown`, `generating`, `generated`,
`downloaded`, `review_pending`, `rework_required`, `accepted`, and
`delivered`.

Transitions require facts: a task/result for `generated`, a verified local
file for `downloaded`, matching review evidence for `accepted`. An unavailable
capability blocks only its dependent step. Unknown future states remain intact
and are not guessed into a known state.

远端执行状态与制作阶段分开；生成、落盘、验收和交付分别需要事实支持。提交结果未知时不能重提
冒险重复计费；能力缺失只阻塞依赖它的步骤。未知版本或状态保留，不猜测归一化。

On “make shot 2 a night scene,” resolve the stable shot ID, preserve previous
versions and good shots, create a new prompt/version, invalidate affected
reviews, regenerate only that shot, reassemble affected output, and recheck
it.

On resume, read the manifest and query existing host/remote jobs before any
submission. After download, resume review locally rather than generating
again.

“第二镜头改成夜景”应定位稳定镜头 ID，保留旧版本和其他合格镜头，新建提示词版本，撤销受影响的
验收证据，仅重做该镜头并重新合成、验收。恢复时先读清单、查原任务；已落盘则继续本地检查。

Skill acceptance scenarios: fresh multi-shot creation; one-shot revision;
restart while generating; restart after download; ambiguous submission;
missing audiovisual reviewer; and unknown manifest fields/versions.

Test actual discovery/invocation, manifest reread across controller
recreation, scoped tool execution, and provider-prefix stability. Behavior
evaluation should check artifacts and decisions, not just match skill prose.

技能验收覆盖新建、局部修改、生成中恢复、下载后恢复、提交未知、缺少视听工具和未来格式；
检查真实调用、落盘重读、执行范围和模型请求前缀，以产物和决策评估效果，不能只断言文案存在。

## Later work / 后续工作

### Stage 4 — Artifact presentation / 第四阶段：产物呈现

Project the same artifact/version records into preview, download, provenance,
review coverage, and superseded-version UI. Ensure foreground and recovered
background completion both expose media. Reuse the shared controller contract;
do not implement a desktop-only result store.

Keep generated/private URLs and network authority out of display-side
inference.

前台和恢复后的后台任务都应呈现同一产物记录，支持预览、下载、来源、验收覆盖和版本关系；
通过共享控制器提供，不另建仅供 Desktop 使用的结果存储，也不由界面推导网络授权。

### Stage 5 — Script orchestration and service adapters / 第五阶段：脚本编排与服务适配

Add typed results to `run_script` compatibly, then host-scheduled bounded
concurrency, task waits, and partial failure recovery. Every nested call
retains the normal permission/cancellation/accounting pipeline. Script-local
variables are not durable task state.

Add service adapters for explicitly declared query/idempotency contracts;
ordinary JSON `job_id` is not MCP Tasks. Never automatically replay an
ambiguous billable submission without a service-backed
deduplication/reconciliation guarantee.

先兼容补充类型化结果，再补宿主受控并发、等待和局部失败恢复；嵌套调用继续走统一权限、取消和
计量管线。脚本局部变量不能当持久任务状态。普通 JSON job ID 需要显式适配查询和幂等契约，
不能假定支持 MCP Tasks；计费提交结果未知时，没有服务保证就不能自动重提。

### Stage 6 — Model/service evaluations / 第六阶段：模型与服务评测

First run an offline deterministic service harness. Then, when credentials and
cost scope are available, run a fixed task set against actual services.

Include a text/tool planner with external review and a capable multimodal
planner; include DeepSeek Flash as the preferred cost/cache benchmark where
available. Pin model version, service version, prompts, tool catalog, budgets,
and review rubric. Report live evidence separately from fixtures.

先运行离线确定性服务测试，再在凭据和费用范围明确时跑真实服务。覆盖文本编排加外部验收、支持
多模态的模型，并在可用时重点比较 DeepSeek Flash。固定模型、服务版本、任务、工具、预算和
验收标准；离线测试与真实服务结果分开报告。

Define the population and exclusions for every metric:

- Accepted deliveries/requested tasks; successful local edits/requested edits;
  original-task takeovers/eligible restart trials.
- Duplicate submissions per production; model turns; elapsed time from request
  to accepted local file; total model+service cost.
- Provider-reported cache-hit/miss input tokens over measured requests.

Missing provider usage is unknown, not zero. Prefix equality is a structural
guard, not a measured live cache-hit rate. Count unverified required scopes as
incomplete acceptance.

指标明确分母与边界：成片通过率、局部修改成功率、符合条件的重启接管率、重复提交、模型回合数、
请求至本地验收完成耗时、模型加服务总费用、provider 报告的缓存命中及未命中输入 token。
缺少用量数据不是零；字节稳定测试也不等同真实命中率。必需项未验证不能计入已验收成片。

## Compatibility and completion gates / 兼容性与完成门槛

### Start here / 接手入口

Read `REASONIX.md`, this plan, and the live PR diff. Preserve any unrelated local
edits. Integrate current upstream first, then finish download hardening/wiring,
technical acceptance, and the production skill in that order.

先读项目约定、本方案和实时 PR 差异，保留无关本地改动。先整合上游，再按下载加固与接入、
技术验收、制作技能的顺序推进。

Useful focused commands for this checkpoint / 当前检查点的定向命令：

```sh
go test ./internal/tools/builtin -run 'TestDownloadArtifact|TestArtifactSource'
go test ./internal/ext/plugin -run 'TestDurableMCPTask|TestRemoteTaskRecovery'
go test ./internal/tools/jobs -run 'TestRemoteTask|TestRemoteTerminal|TestRemoteInline'
go test ./internal/assembly/boot -run 'TestEffectMCPStructuredResult|TestEffectMCPRemoteTask'
go test ./internal/contract/tool -run TestBuiltinToolContractDocumentation
go test ./internal/tools/builtin -run TestBuiltinSchemaSurfaceStaysWithinBudget
go test ./internal/tools/builtin -run 'TestCheckVideo|TestConditionalMediaSchemas'
go test ./internal/assembly/boot -run 'TestEffectDownloadArtifact|TestEffectCheckVideo|TestMediaDecoders'
```

`TestCheckVideoRealFFmpegFailsATruncatedTail` skips where ffmpeg is not installed.

`TestCheckVideoRealFFmpegFailsATruncatedTail` 在未安装 ffmpeg 时跳过。

| Contract / 契约 | Existing data / 旧数据 | New behavior / 新行为 | Older reader / 旧读取器 |
| --- | --- | --- | --- |
| Session/job metadata / 会话任务元数据 | Unchanged / 不变 | Existing paths plus optional sidecars / 原流程加可选旁文件 | Existing behavior / 原有行为 |
| Retained MCP result / 保留结果 | Plain text still valid / 纯文本仍有效 | v1 envelope read; unknown versions retained / 读取 v1，保留未知版本 | Ignores sidecar / 忽略旁文件 |
| Download receipt / 下载回执 | Optional / 可缺省 | Prototype v1, create-only / 原型 v1，只新建 | Ordinary file, no automatic execution / 普通文件，不自动执行 |
| Production manifest / 制作清单 | No manifest required / 无清单仍可使用 | Proposed v1, preserve unknown fields / 拟定 v1，保留未知字段 | Ordinary workspace file / 普通项目文件 |
| Tool definitions / 工具定义 | Existing definitions unchanged / 原定义不变 | Deterministic new capabilities / 新能力定义确定 | New calls unavailable; no replay fallback / 新调用不可用，不伪造回退 |

Before ready-for-review: integrate the current `studio` base; preserve
unrelated local edits; inspect the complete three-dot diff; run format, vet,
lint/check, owning tests, root/desktop tests where affected, targeted races,
provider effect tests, and platform-appropriate media/network/filesystem
tests.

Preserve the existing `read_video` native URL boundary and session ownership
checks. Document any missing live/platform evidence. Do not widen ratchet
baselines to land work.

转正式评审前整合当前 `studio`，保留无关改动，审查完整三点差异；完成格式、vet、lint/check、
所属包与受影响模块测试、race、provider 边界及跨平台媒体网络文件系统验证。保留原视频 URL
授权和会话所有权边界，明确尚缺的真实服务或平台证据，不放宽静态检查基线。

## Design references / 设计参考

- [DeepSeek Harness tool contracts](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/core/tools/README.md): canonical values versus rendering and host-mediated tool composition / 规范值与渲染分离、宿主统一编排。
- [DeepSeek Harness MCP client](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/mcp/mcp-client/README.md): inspect supported protocol scope rather than assuming task durability / 以明确协议能力为准，不假定任务可持久恢复。
- [Anthropic: long-running application harnesses](https://www.anthropic.com/engineering/harness-design-long-running-apps): persistent handoff and independently verifiable progress / 持久交接与可独立验证的进展。
- [Anthropic: code execution with MCP](https://www.anthropic.com/engineering/code-execution-with-mcp): compact model context around programmatic tool use / 程序化工具调用与精简模型上下文。

These are design inputs. Reasonix's existing contracts and the tests above
determine the implementation; no provider-specific harness is a prerequisite.

这些是设计参考；实现以 Reasonix 已有契约和上述验收为准，不依赖任何特定厂商的 harness。
