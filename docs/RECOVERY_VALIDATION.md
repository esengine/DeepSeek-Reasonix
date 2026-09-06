# Protocol and recovery validation / 协议与恢复验证

Implementation date / 实施日期: 2026-09-05.

## Reference scope / 参考范围

Pi is pinned to `9841914c71a74d81abe07f751aefd271fd924e63`. The executable
comparison uses its `packages/ai/src/utils/retry.ts` `retryAssistantCall`
helper, with an injected zero delay. It is not an end-to-end comparison of the
Pi runtime, and does not measure either product's production recovery rate.

Pi 固定为上述提交；可执行对照使用其重试辅助函数，测试中等待设为零。
这不是完整 Pi Agent 的端到端对照，也不是线上恢复率或费用基准。
主会话持续等待是 Reasonix 的扩展，不是 Pi 的默认策略。

| Fault / 故障 | Pi helper requests / 请求数 | Reasonix requests / 请求数 | Outcome / 结果 |
| --- | ---: | ---: | --- |
| Two temporary service failures, then success / 两次临时服务故障后成功 | 3 | 3 | Automatically completes / 自动完成 |
| Exhausted quota / 配额耗尽 | 1 | 1 | Stops immediately / 立即停止 |
| Persistent interrupted stream / 持续断流 | 4 | 4 | Finite failure; no endless regeneration / 有限失败，不无限重生成 |

For the recoverable fixture, additional requests are 2 and manual continuation
is 0. Reasonix's scheduled quick backoff is 2 + 4 seconds; the persistent failure
fixture schedules 2 + 4 + 8 seconds. Test clocks avoid actually waiting that long.
Missing provider usage remains unknown: these fixtures do not establish token
cost, real recovery latency, or a statistically meaningful recovery percentage.

可恢复用例增加两次请求，人工继续次数为零；名义退避总时长为 6 秒。
持续失败用例的名义退避总时长为 14 秒。测试时钟跳过实际等待。
缺失的供应商用量保持未知，不把它记为零费用；这些用例不构成 token
成本、真实恢复耗时或有统计意义的恢复率测量。

## Deterministic coverage / 确定性覆盖

- Compatible missing reasoning: no regeneration before the tool, and one tool
  execution. / 兼容协议缺少 reasoning 不额外生成，工具执行一次。
- Mixed network and replay failures share four total attempts; cancellation
  prevents a late completion from starting tools. / 混合失败共用四次请求上限，
  取消后的迟到响应不能启动工具。
- Main conversations wait after quick retry exhaustion; subagents, planners,
  partial streams and unknown tool outcomes cannot enter that wait.
  / 主会话可持续等待；子任务、规划、部分断流和未知工具结果不能进入该状态。
- Auxiliary calls use 2/4/8-second finite backoff, aggregate usage, and suppress
  failed partial text. / 辅助调用有限退避、汇总用量、排除失败的部分文本。
- A failed durable intent prevents mutation, including directory creation.
  Verification checks every recorded target again before skipping a write.
  Conflicts, changed symlink destinations, unavailable/replaced transports and
  unknown evidence versions cannot prove success. / 意图持久化失败不开始写入或
  创建目录；跳过写入前重新核验所有目标，冲突、符号链接换目标、原通道不可用或
  被替换、未知证据版本均不能证明成功。
- Raw future-version write evidence survives serialization and is excluded
  from model messages. / 未知版本原始证据往返保留，不进入正常模型消息。

Tests: `internal/agent/pi_recovery_test.go`,
`internal/agent/write_recovery_test.go`,
`internal/provider/recovery_test.go`,
`internal/provider/auxiliary_recovery_test.go`,
`internal/tool/builtin/write_recovery_test.go`, plus existing protocol,
checkpoint, session-generation, and frontend stream suites.

## Validation boundaries / 验证边界

At the initial local-validation stage no live credential was available. The
official endpoint follow-up below supersedes that limitation, but does not
reproduce the reporter's exact Windows session or every custom gateway.
The real Serve-page check was attempted through the available in-app and Edge
browser channels; both blocked the loopback test URL before loading the page.
Frontend type and stream-state tests are separate from a real UI smoke test.
Native Desktop visual behavior still requires that smoke test.

初始本地验证阶段没有可用密钥；下方补充了官方端点实测，但仍未复现反馈者的
完整 Windows 会话，本地与官方端点通过不等同于所有中转场景均已解决。
尝试通过内置浏览器和 Edge 检查实际 Serve 页面时，两条通道均在加载前拦截
本地测试地址。前端类型与流状态测试不能替代真实界面检查，原生 Desktop
的视觉表现仍需完成界面冒烟验证。

## Local checks / 本地检查结果

Passed / 已通过:

- Root module: `go test -p 1 ./... -timeout 180s`; the final affected Agent,
  Provider, built-in tool and event-wire packages were also rerun successfully.
- Desktop module: `go test ./... -timeout 240s`.
- `make lint` and `git diff --check`.
- Frontend `tsc --noEmit` and `test:stream`.
- Targeted `-race` checks across Agent, Provider, built-in tools and Controller,
  including cancellation, unknown writes, auxiliary retry budgets, concurrent
  snapshots and session-generation changes.

根模块与 Desktop 全量测试、最终受影响包复测、静态检查、前端类型与流状态
测试均通过。恢复与取消、辅助重试、文件核验、并发快照、会话切换的定向
race 检查通过。真实界面与服务端验证仍受上节边界约束。

## Anthropic compatibility follow-up / Anthropic 兼容性补充

The adapter distinguishes native Claude signature requirements from unknown
Anthropic-compatible gateways. Complete unsigned native non-tool history can be
converted to assistant text in the request view; tool activity, mixed proofs,
redacted data and incomplete reasoning remain protected. Gateways preserve
received unsigned thinking when replay is enabled, and explicit DeepSeek
contracts remain strict. No empty signatures are fabricated.

Additional deterministic HTTP/SSE fixtures in
`internal/agent/anthropic_compatibility_e2e_test.go` verify:

- Missing or unsigned thinking on a custom adaptive gateway: two HTTP requests
  for one tool round and the final answer; one tool execution, no regeneration.
- A server rejection of unsigned history: three HTTP requests, one tool
  execution, with completed-tool facts in the repaired request and original
  reasoning retained locally.
- A compatible native text conversion does not claim the missing-reasoning
  recovery incident. Adapter tests cover immutable history, idempotent
  conversion, signed/redacted block preservation, and rejection of unsafe input.

新增测试区分原生 Claude、未知 Anthropic 网关和显式 DeepSeek 契约。模拟网关
缺失或返回 unsigned thinking 时，一个工具轮加最终回答共两次 HTTP 请求，工具
执行一次；模拟服务端拒绝旧 thinking 时，共三次请求，工具仍只执行一次，修复
请求携带已完成事实，本地保留原始 reasoning。兼容文本转换不消耗严格恢复预算。

These are simulated servers, not live endpoint acceptance tests. No DeepSeek,
Anthropic or OpenCode Go API credentials were available for this follow-up.
本轮为模拟服务端验证；环境未提供上述供应商密钥，不能据此宣称 #9808 的反馈者
端点或所有兼容网关已通过实测。

## Official endpoint investigation (2026-09-05) / 官方端点实测

This follow-up uses a user-authorized credential only against
`api.deepseek.com`, on `deepseek-v4-flash` and `deepseek-v4-pro`. It covers
`/chat/completions`, `/responses`, and `/anthropic/v1/messages`. Credentials
are supplied in memory to isolated test processes; no credential file is
created. Only synthetic marker tools and confined temporary-file writes are
available. No shell, MCP, credential reader, or other provider is exposed.

本次使用用户授权的官方密钥，覆盖 Flash、Pro 及三种协议。密钥仅在测试进程中
传递；模型只能调用固定标记工具或临时目录内的文件写入，不提供 shell、MCP 或
凭据读取能力。以下区分原始服务端契约、真实模型加本地故障注入、确定性回归。

### Raw replay contract / 原始回放契约

The initial 44 direct, non-streaming HTTP probes all returned 200. Those probes
reused provider-issued call IDs. A second set of 30 probes replaced call IDs,
with full-reasoning controls to establish that the replacement IDs themselves
were valid. Twenty-two returned 200; eight deliberately invalid requests
returned the expected 400. Both models produced the same distinctions:

| Historical input / 历史输入 | Chat | Anthropic Messages | Responses |
| --- | --- | --- | --- |
| Original call IDs; omit reasoning / 原始调用 ID，省略 reasoning | 200 | 200 | 200 |
| Replacement call IDs; full reasoning / 替换调用 ID，完整 reasoning | 200 | 200 | 200 |
| Replacement call IDs; omit reasoning / 替换调用 ID，省略 reasoning | 400 `reasoning_content` | 400 `content[].thinking` | 400 `reasoning_text` |
| Replacement call IDs; explicit empty field/block/lists / 替换调用 ID，显式空字段、块或列表 | 200, empty string | 200, empty thinking block | 400, empty content/summary lists |

All rejection messages require the named content to be passed back. The
ID-dependent difference is evidence of server behavior, not proof of its
internal storage/cache implementation or a durability guarantee. A successful
request with an original call ID is **not** sufficient evidence that missing
reasoning will always be accepted after restart, ID normalization, or gateway
translation. Do not fabricate opaque Responses items or extend Chat's empty
field rule to other protocols from these results.

三种协议在替换调用 ID 后均能复现真实的回放 400。原始 ID 下成功，不能证明
历史重载、ID 转换或网关转发后仍可省略 reasoning；服务端内部如何找回这些
信息未验证。Responses 的“空列表”与合法的 reasoning item 并不等价。

The official [thinking-mode guide](https://api-docs.deepseek.com/guides/thinking_mode/)
continues to require full historical reasoning with tools. The
[Anthropic compatibility guide](https://api-docs.deepseek.com/guides/anthropic_api/)
describes Messages compatibility. Healthy history therefore retains all
received proof. This investigation does not relax native Claude signatures or
explicit strict DeepSeek Anthropic contracts.

官方文档仍要求带工具请求完整回传历史 reasoning。正常历史继续保留真实内容；
本次没有放宽原生 Claude 签名要求，也没有修改显式严格 Anthropic 契约。

### Defects found and fixed / 实测发现及修复

1. An EOF inside a JSON data line was a fatal decode error in Chat and Messages.
   The shared stream scanner now distinguishes an unterminated JSON prefix from
   a malformed complete event. The former uses bounded stream recovery; the
   latter still fails. Partial tool calls never execute.
2. The actual Responses rejection names `reasoning_text`, which the replay-error
   parser did not recognize. It now enters the existing bounded history repair,
   preserving completed-tool facts and excluding invalid protocol history.
3. Request-only and byte-estimated usage could lose the unknown-usage flag.
   Missing provider usage now remains unknown through estimation and aggregation;
   request counting and known token telemetry remain available.
4. Strong history repair retained completed-tool names but discarded their
   outputs, causing real models to repeat the tool or be unable to answer.
   Recovery now includes bounded original model-visible results as escaped,
   explicitly untrusted JSON; raw/local-only output stays excluded. Only a
   repaired fault prefix changes; healthy requests are untouched.
5. The write-intent hook was attached to the outer execution context while
   dispatch used the already prepared tool context. It is now installed on the
   actual dispatch context after permission and preparation. A failed intent
   checkpoint prevents the write from starting.

发现并修复五处遗漏：断流 JSON 误分类、Responses 回放错误漏识别、未知 usage
标记丢失、历史修复丢掉实际工具结果、写入持久化钩子未传入真正执行上下文。
只有故障修复视图新增有界工具结果；正常提示词、工具 schema、字段顺序与健康
历史不变。回归测试还验证了结果转义、RawContent 排除、持久化失败禁止写入。

Deterministic regressions: `internal/provider/stream_scanner_test.go`,
`internal/provider/reasoning_replay_error_test.go`,
`internal/agent/stream_fragment_recovery_test.go`, and
`internal/agent/cancel_test.go`. Live entrypoints are build-tagged `live` and
credential-gated. The old live missing-reasoning expectations were updated to
assert zero extra generation for compatible Chat/Responses turns. Independent
search tests now pin the supplied process credential explicitly instead of
silently skipping because an isolated home has no global credential file.

### Measurements and qualification / 指标与验收结果

Worktree base: `1b4f9ae8324413d04ae272ceadb86ad49ffded2e`, plus local changes;
these results do not describe a published release. All paid calls were
sequential. Root package tests replace retry sleeps with a controllable test
sleeper, so the following live latency numbers exclude the production 2/4/8s
backoff. The proxy buffers a real upstream response before injecting a fault;
this is not a measurement of a naturally occurring server outage.

| Suite / 用例组 | Observed result / 结果 |
| --- | --- |
| Raw wire contract / 原始 HTTP 契约 | 74 requests: 66 HTTP 200 and 8 expected HTTP 400; 25,469 input and 1,860 output tokens reported across metered responses. Anthropic input includes cache-read/create tokens. The eight 400s have unknown usage. |
| Recovery matrix / 恢复矩阵 | 54 cases matched their expected outcomes: 122 client HTTP attempts, 116 official upstream requests, 46 actual marker-tool executions. Six 503s were local injection without upstream calls. |
| Recoverable faults / 可恢复故障 | 20/20 continued automatically, zero manual continuation, one extra attempt per fault. This includes six stream cuts, six temporary 503s, six actual server replay rejections and two single missing-thinking strict turns. |
| Recovery latency / 恢复耗时 | Whole recoverable scenarios: median 2.949s, range 0.994–6.591s; this includes normal tool/final requests, not just the recovery request, and excludes production retry sleeps. |
| Protective stops / 保护性停止 | Six cancellations executed no tool; two persistent strict Anthropic missing-thinking cases stopped after two requests and executed no tool. These are expected stops, not successful automatic recoveries. |
| Compatible missing reasoning / 兼容缺失 reasoning | Eight Chat/Responses missing-once/persistent cases completed with two requests and one tool execution; no reasoning regeneration. |
| Matrix usage / 矩阵用量 | 41,161 input and 4,347 output tokens recorded, including local estimates for incomplete requests. Twenty-four cases retained unknown-usage metadata. Extra retry tokens were not independently metered; no exact monetary total is claimed. |
| Conversation continuity / 连续会话 | Six Flash/Pro × protocol combinations, six user turns each, one save/load each: 72 requests, 36 tool executions, zero retries, 97,336 input / 2,053 output tokens. Healthy history, tools and settings stayed byte-stable across requests and reload. |
| Cache / 缓存 | Continuity aggregate: 88,320 cached / 97,336 input tokens (90.74%). A separate large-tool-output Chat test preserved its bounded stable prefix; its last two turns hit 9,472/9,611 and 9,856/9,882 tokens. Local RawContent sentinel was absent from requests. |
| Write effect before result checkpoint / 已写入但结果未保存 | Three protocols passed: one durable intent and one actual disk write each. Reload produced an unknown-result placeholder, not a fabricated completed result; verification prevented a second disk write. |
| Independent search / 独立搜索 | Search returned eight structured sources; default Chat → independent Messages search → Chat final completed. Standalone search reported one request, 17,238 input / 939 output tokens. |

Additional earlier checks passed: 20 Responses Flash/Pro tool loops, Chat and
Responses reasoning-removal probes, official Messages tool/history/search
round-trips, and cancel-after-tool save/load continuation.

真实恢复矩阵的 54 个场景均达到各自预期；其中 20 个可恢复故障自动继续，8 个
取消或严格协议持续缺失场景按设计停止，不能将后者算成“自动恢复成功”。主矩阵
没有重复执行工具。统计使用固定合成任务，不能据此估算所有真实任务的恢复率。
重试等待在测试中被替换，因此这些时长不是生产环境故障的真实等待时间。

A subsequent repair-boundary check found that anchoring the overlay to the
already stripped history could omit a trailing removed tool pair. The boundary
now anchors to the original source history. Completed-result facts identify
originating user turns, avoiding treating old work as fulfillment of a new task.
The deterministic follow-up asserts that the next request retains the repaired
view and the actual completed output.

后续还修正了历史修复边界：定位到原始历史，而不是已经删除工具轮的结果视图。
这样下一轮不会立即重新带回刚删除的错误协议历史；结果标记所属用户轮次，避免
把历史工作误当成新任务已经完成。

**Observed model variability:** the six real post-repair continuation checks
completed, but one Flash/Chat sample requested the read-only marker a second
time (five requests / two executions instead of four / one), failing the strict
no-repeat assertion. Three diagnostic repeats of that exact case then passed
without a duplicate; the added counters showed one execution before and after
continuation in those repeats. The first observation remains a limitation,
not erased by the passing repeats. No generic same-arguments deduplication was
added: a new request can legitimately require a fresh read. The verified
no-repeat disk-write result must not be generalized to every tool or model call.

补充的六组“修复后再继续”均完成任务，但其中一个 Flash/Chat 样本重复调用了
一次只读标记工具，未达到严格的零重复断言；随后三次定向复测没有复现。
该观察仍保留为边界，不因复测通过而抹去。没有按相同参数永久去重，因为用户
新请求可能需要重新读取。文件未重复落盘不等于所有模型都不会重复调用工具。

Final deterministic checks passed: root and Desktop module suites, targeted
race checks for stream/usage/replay/write-intent recovery and controller write
checkpoint reload, `make lint` (0 Go issues; existing repolint baseline unchanged),
and `git diff --check`. Live tests remain opt-in under the `live` build tag;
model-dependent no-repeat assertions can fail as described above.

最终根模块、Desktop 全量测试及恢复/取消/写入检查点的定向 race 检查通过，静态
检查与差异检查通过。真实测试受 `live` 标签保护；上述依赖模型选择的严格零重复
断言仍可能失败。

This is official API evidence on macOS plus controlled local faults. It does
not validate the reporter's complete Windows session, native Claude, arbitrary
custom gateways, unknown shell/MCP effects, native Desktop visual behavior, or
Pi/OpenCode against the same live account. Issue #9808 cannot be declared solved
for every deployment from these samples; no issue closure, push or release was
performed.

本次覆盖 macOS 下官方 API 及本地可控故障，不等同于反馈者完整 Windows 会话、
任意中转、原生 Claude、未知 Shell/MCP 副作用或原生界面的全面实测。没有关闭
issue、提交、推送或发布，不能据此宣称 #9808 的所有部署场景均已解决。
