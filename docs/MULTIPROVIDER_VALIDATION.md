# Multi-provider live validation / 多供应商真实端点验证

Date / 日期: 2026-09-05. Local development checkout; no commit, publication or
issue closure is implied. / 当前本地开发版本，未提交、发布或关闭 issue。

## Scope and method / 范围与方法

The user authorized live requests against LongCat, official DeepSeek, Zhipu
Coding Plan CN, and OpenCode Go. Each process receives only its selected
provider's credential, with disposable HOME/REASONIX_HOME directories. Keys
are never stored in the checkout or report. Model-facing tools consist only of
fixed markers, read-only argument/error fixtures, or a built-in writer confined
to a disposable directory. There is no shell, MCP or credential-reading tool.

用户授权使用四家供应商的真实凭据；每个测试进程只收到该供应商的密钥，使用
独立临时目录。模型工具仅为标记、只读参数/错误测试或限定临时目录的文件写入，
不提供 shell、MCP 或凭据读取能力。请求串行执行，有每用例期限和请求上限。

Provider adapters and the real Agent loop execute the requests. A local proxy
can cut an actual upstream SSE response, remove reasoning, inject one HTTP 503,
or cancel just before commit. These are controlled faults applied to real
responses, not claims about naturally occurring outages. Ordinary retry delays
are shortened by the test clock; elapsed values do not measure production
2/4/8-second backoff. Discarded attempts without final usage remain unknown.

实测经过真实适配器与 Agent 循环；故障代理作用于真实响应，不向公共服务注入
故障。测试时钟缩短退避，耗时不代表生产环境的恢复时延；缺少终态用量的请求
保持“未知”，不能根据报告中的 token 总数推算准确账单。

### Protocol routing / 协议路由

| Provider / 供应商 | Routes / 路由 | Contract / 契约 |
| --- | --- | --- |
| LongCat | `/openai/v1/chat/completions`, `/anthropic/v1/messages` | LongCat-2.0; Bearer authentication for both / 两者均使用 Bearer |
| Zhipu Coding Plan CN / 智谱 | `/api/coding/paas/v4/chat/completions`, `/api/anthropic/v1/messages` | Seven GLM models; CN plan endpoint, not the international Z.AI route / 七个 GLM 模型，使用中国区套餐端点 |
| Official DeepSeek / 官方 | `/chat/completions`, `/responses`, `/anthropic/v1/messages` | Flash/Pro coverage in the earlier report; this extension adds the vision SKU's text/tool and image paths / 既有报告覆盖 Flash、Pro，本轮增加视觉 SKU 的文本工具与图像输入 |
| OpenCode Go | `/zen/go/v1/chat/completions`, `/zen/go/v1/messages`, `/zen/go/v1/responses` | Model-specific routing; Messages uses `x-api-key`, Chat/Responses use Bearer / 按模型选择路由与鉴权；另测 DeepSeek 自定义 Messages/Responses |

Routing references: [LongCat API](https://longcat.chat/platform/docs/APIDocs.html),
[Zhipu Coding Plan](https://docs.bigmodel.cn/cn/coding-plan/using5-1),
[OpenCode Go endpoints](https://dev.opencode.ai/docs/go/).
The model catalogs were fetched live, but a successful catalog request does not
prove inference permission or remaining quota. / 模型目录已实时获取；目录返回
200 不代表生成请求有权限或额度。

## Credential and quota gates / 凭据与额度边界

LongCat's direct generation routes returned HTTP 402, insufficient token quota.
The initial batch made 14 rejected requests, executing no model tool; these do
not count as recovery coverage. The harness now stops subsequent scenarios for
a route after a confirmed quota gate. LongCat-2.0 on OpenCode Go is a separate
route and did complete its baseline and recovery tests.

LongCat 官方生成接口仍因 token 额度不足被阻塞；初始批次 14 次拒绝请求没有
执行模型工具，不能算作恢复覆盖。测试器已增加额度门槛后的停止规则。Go 上
LongCat-2.0 的通过不能替代官方接口验证。

OpenCode Go initially returned CreditsError/HTTP 401. After the user renewed
the subscription, three correctly authenticated direct protocol probes returned
200 and the funded model matrix passed. No billing settings were changed by
Reasonix or by these tests. / 用户重新订阅后，三种正确鉴权的原始请求均返回
200，后续模型矩阵通过；测试没有修改账户计费设置。

## Initial matrices / 首轮矩阵

| Matrix / 矩阵 | Cases / 用例 | Client HTTP / 客户端请求 | Upstream HTTP / 真实上游 | Outcome / 结果 |
| --- | ---: | ---: | ---: | --- |
| Zhipu baseline, 7 models × 2 protocols / 智谱基础往返 | 14 | 28 | 28 | All completed; one tool each / 全部完成，每例一个工具 |
| OpenCode Go funded baseline, 27 models / Go 重新订阅后基础往返 | 31 | 62 | 62 | All completed; no retries / 全部完成，无重试 |
| Official DeepSeek vision SKU text/tool baseline / 官方视觉型号文本工具往返 | 3 | 6 | 6 | All three protocols completed / 三种协议均完成 |
| Zhipu recovery + continuity, 3 models × 2 protocols / 智谱恢复与连续会话 | 36 | 102 | 96 | 12/12 injected recovery cases completed; 6 cancellations safe / 12 个恢复用例完成，6 个取消未执行工具 |
| Official vision SKU recovery + continuity / 官方视觉型号恢复与连续会话 | 18 | 52 | 49 | 7/7 injected recovery cases completed; 3 cancellations safe / 7 个恢复用例完成，3 个取消未执行工具 |
| OpenCode Go recovery + continuity, 8 models / Go 恢复与连续会话 | 50 | 148 | 139 | 19/20 injected recovery cases completed; two Kimi tool-omission observations / 19 个恢复用例完成，Kimi 有两次未调用工具观察 |

Go's 50 cases comprise 45 passes, two failures and three unexercised
missing-reasoning probes. One Kimi K3 failure happened after an injected cut;
another omitted the tool on the first response, so the intended second-request
503 was never injected. They must not both be counted as transport-recovery
failures, nor counted as completed tasks. Diagnosis and repeat samples are
reported separately. The low-effort Kimi sample differs from the preset's max
setting; both levels are tested without changing the user's configuration.

Go 的两次 Kimi 观察分别为断流重试后未调用工具、第一轮就未调用工具（没有机会
注入后续 503）。两者不能都记为网络恢复失败，也不能记为完成任务。低档位样本
与预设的 max 档位不同，另行对照，不修改用户设置。未触发故障的用例不算通过。

The Zhipu recovery matrix initially flagged three Anthropic cache-prefix checks.
All model turns and tool counts completed; the failure was the overly strict
cache-marker comparison described below. Corrected continuity replays are
reported separately. Initial permanent-read-error tests similarly exercised an
unintended host transient-read retry and are not counted as model duplicates.

智谱首轮有三个 Anthropic 缓存断言报错，但所有模型轮与工具次数符合预期；
缓存标记检查校正后的复测另列。永久读取错误的旧测试也不混入模型重复调用统计。

Recorded input/output totals for the three recovery matrices are respectively
20,227/1,455 (Zhipu), 19,588/1,303 (official vision SKU), and 44,792/3,640 (Go).
They include estimates for incomplete attempts; 18, 9 and 29 cases respectively
contain unknown usage. Successful recovery-case elapsed medians are 10.093 s,
2.028 s and 4.055 s with shortened test waits. Each successful injected recovery
used one extra model request. No manual continuation was used to rescue a failed
sample; incomplete samples remain failures. These are selected-case results,
not production reliability percentages or accurate bills.

三个恢复矩阵记录的输入/输出 token 如上，包含中断估算；未知用量用例分别为
18、9、29。成功恢复用例中位耗时分别约 10.093、2.028、4.055 秒（测试退避缩短）。
每个成功恢复用例增加一次模型请求；没有通过人工继续挽救失败样本。不能将这些
受控结果解释为线上成功率或准确费用。表中请求数不包括目录查询、旧额度探测、
参数/文件/搜索/图像专门测试及后续对照，不能当作整个测试任务的请求总数。

## Follow-up results / 复测与专项结果

| Check / 检查 | Coverage / 覆盖 | Outcome / 结果 |
| --- | --- | --- |
| Zhipu corrected Anthropic continuity / 智谱连续会话 | Three models, three user turns each, save/reload / 三模型各三轮并保存重载 | 18 HTTP requests, nine tools, all passed / 全部通过 |
| Canonical Unicode/newline arguments and permanent tool errors / 参数与永久工具错误 | Six Zhipu, five Go, three official vision protocol cases / 共十四组合 | Each made two requests and executed alpha/beta/unavailable once / 各两次请求，三个工具标记各执行一次 |
| File side effect followed by interrupted result persistence / 写入后结果持久化中断 | Six Zhipu, five Go, three official vision protocol cases / 共十四组合 | All passed: one durable intent and one actual disk write per case / 每例一个持久意图、一次真实写入，恢复不重复写 |
| Actual generated image input / 真实图像输入 | Official vision SKU over three protocols; Go vision SKU Chat / 四组合 | Left red/right blue correctly identified; one request per sample / 全部识别正确，每样本一次请求；Go 另有一个通过的重复样本 |
| Go native DeepSeek Flash search / Go 原生搜索 | Messages + Responses | Messages: ten sources; Responses: search/echo completed but no structured sources / 后者搜索及工具往返完成，来源不可用 |
| Actual server replay rejection / 服务端真实回放拒绝 | Go Flash/Pro × three protocols / 六组合 | Chat/Responses four recoveries; Messages two opaque 400 stops / 四例自动恢复，两例因无错误详情停止 |

The file fixtures cancel after the write has actually happened, restore the
checkpoint written before its result, and verify the desired target before
continuing. Their one-write assertion refers to actual writes, not just model
call counts. It does not cover shell/MCP/remote/editor writes.

文件测试确实在写入完成、结果尚未持久化时中断，再恢复旧检查点并核验目标；
“没有重复写入”统计实际磁盘写入，不只是模型调用次数。未覆盖 shell、MCP、
远程或编辑器缓冲区写入。

### Kimi and Go protocol diagnostics / Kimi 与 Go 协议诊断

The additional low-effort Kimi matrix repeated baseline, cut, follow-up 503 and
missing reasoning three times each: 10/12 completed, 27 HTTP requests, ten tool
executions and five retry events. The two omissions had no tool-call field on
the original wire. All three cut samples recovered; all three actual removed-
reasoning samples completed after the test stripper was corrected to include
Chat's `reasoning` alias. The max-effort control repeated baseline/cut three
times each: 5/6 completed, 14 requests, five tools, three retries. The failed
baseline also had no tool call on the wire. Changing effort alone is not a fix.

Kimi 的补充 low 档位 12 例完成 10 例，max 档位 6 例完成 5 例；失败样本原始
响应也没有工具调用，因此不是适配器漏解析。两组各三个断流样本均恢复。
reasoning 缺失测试补齐字段别名后，三个实际移除样本均完成。

A prompt-only experiment, inspired by OpenCode's explicit action requirements,
added a stable instruction to execute requested tools and never invent results.
At max effort, three baseline and three cut/retry samples all completed: 15 HTTP
requests, six physical tool calls and three retries. The original max control
completed five of six. This small sequential sample is promising, not a
statistically established improvement or an OpenCode runtime comparison. The
experiment does not change Reasonix's production prompt or thinking settings.

借鉴 OpenCode 行动提示的短提示实验在 max 档位六例全完成；原提示对照为五例。
这是小样本顺序实验，不能据此声称稳定收益，生产提示未改。另一个 Kimi 带参数
测试三次中两次通过，一次 Unicode/换行值不符合测试要求；每次都调用了三个
工具各一次，六次请求，无网络重试。该测试衡量首次参数正确性，并明确禁止
重复调用，不能当作一般参数自动纠错能力的结论。

The Go replay-rejection batch made 16 requests with six physical tool executions.
All four Chat/Responses cases followed 200 → 400 → 200 without replaying the
tool. Both Messages cases followed 200 → 400 with a body containing only the
model name, retaining the completed tool. The test knows the injected cause;
production clients cannot infer that cause from the opaque body. This remains
an automatic-recovery limitation, not a reason to retry arbitrary 400s.

Go 回放拒绝共 16 次请求、六次工具执行；Messages 的不透明 400 仍是自动恢复
边界。测试器知道故障来源，不代表生产客户端可以猜测真实请求的错误原因。

Two follow-up Responses search samples were inspected at the raw terminal response; the last also verified unchanged raw search-item replay into the next request.
Search actions had queries but no `sources`; the following message had empty
`annotations`. Search execution and continuation passed, but source availability
was not demonstrated. The final harness separately checks raw search-item replay
and marks absent structured sources as unproven, without inferring URLs from
model prose. Initial source-count assertion failures remain recorded.

Responses 搜索缺少来源是原始终态响应可见的事实；最终测试分别验证搜索 item
回放与来源可用性，来源缺失记录为未证明，保留最初严格断言失败的证据。

The OpenCode source comparison and recommended changes are documented in
[OpenCode recovery comparison](OPENCODE_RECOVERY_COMPARISON.md). These are source
findings and Reasonix experiments, not an executed OpenCode-vs-Reasonix benchmark.

## Confirmed fixes / 已确认修复

1. **Quota classification across HTTP statuses.** OpenCode Go returned HTTP 401
   with `CreditsError`/insufficient balance, while LongCat returned HTTP 402
   with `too_many_requests`/insufficient token quota. Exhausted allowance now
   takes precedence over authentication and transient rate-limit labels. Both
   managed and standalone requests stop after one attempt. The localized error
   describes allowance, not an invalid key. Raw billing URLs are excluded;
   status-based `APIError` consumers retain compatibility through unwrapping.
   / 余额与套餐额度错误优先于 401、限流类标签，立即停止；不再提示用户更换
   实际有效的密钥，不输出私有账单链接，并保留既有状态码错误接口兼容性。
2. **OpenCode transport identity.** All three built-in adapters supply the
   client identity and `x-opencode-session`. A bound conversation path maps to
   an opaque stable hash across resume; ephemeral/child sessions have their own
   identity. Standalone calls use a stable client identity. Explicit custom
   headers are preserved. Other vendor routes receive none of these additions.
   / 三种适配器补齐客户端与会话标识；恢复保持稳定，子会话独立，不发送本地
   路径。只调整官方 Go 的 HTTP 头，不改变正常消息、工具 schema 或思考设置。

Cache impact: service-side routing may change when the new header is first
introduced. Conversation/tool bytes remain unchanged. Cache-hit differences
between providers or headers are not an A/B causal benchmark.

缓存影响：首次引入会话头可能改变服务端缓存分组；正常会话正文与工具序列化
保持不变。供应商之间、请求头变化前后的命中数据不构成严格 A/B 因果对照。

## Test harness distinctions / 测试口径校正

- An Anthropic tail `cache_control: {type: ephemeral}` marker normally moves to
  the newly appended tail. The continuity check allows only this exact change
  on the previous tail block. Text, tool arguments/results, reasoning and
  signatures must remain identical; no general recursive field removal is used.
  / 只允许旧尾部的标准临时缓存标记移动，不能借此掩盖历史正文或证明变化。
- A synthetic read error containing “unavailable” initially activated the
  existing one-time read-only transient retry. The permanent-error fixture now
  implements `RetryableToolError() == false`. This distinguishes physical read
  retries from a model replaying a completed tool. / 最初测试错误文本触发了既有
  只读重试；永久错误使用显式类型后再验证，不将其误报为模型重复执行。
- An initial label prompt double-escaped its JSON text, making a literal
  backslash-n interpretation possible. The final argument tests generate the
  JSON literal with `json.Marshal` and require the decoded Unicode/newline
  value. The ambiguous sample is not attributed to the provider adapter.
  / 参数提示最初有双重转义歧义；最终由程序生成标准 JSON，再验证解码值，
  不把该歧义归咎于适配器。
- The first Go Messages probe incorrectly used Bearer authentication. It was
  corrected to `x-api-key`, matching the adapter's actual preset. Historical
  quota/auth probes are separated from funded inference results. / 初版测试的
  Messages 鉴权已纠正；历史拒绝请求不计为有效推理覆盖。

## Evidence and boundaries / 证据与边界

See [the preceding official DeepSeek report](RECOVERY_VALIDATION.md) for its
74 raw replay-contract probes, 54-case recovery matrix, independent search,
cache and write-recovery results. Those observations remain distinct from this
multi-provider extension. Native Claude, arbitrary relays, natural outages,
remote/editor-buffer write recovery and native Windows UI are not proven by
these endpoint tests. A synthetic cancellation does not measure user-interface
stop latency. The generated two-color image proves only this basic image-input fixture, not general visual reasoning.

既有 DeepSeek 报告与本轮结果分别保留。这里不能证明原生 Claude、任意中转、
自然故障、远程/编辑器通道写入恢复或 Windows 原生界面均已通过。测试取消不
等于测量用户界面停止时延；双色图片测试只证明该基础图像样本，不代表通用视觉能力。

## Local verification / 本地验证

- Root full suite: `go test -p 1 ./... -timeout 180s` — passed.
- Desktop module full suite — passed separately.
- `make lint` — passed, without a new lint baseline waiver.
- Focused provider/agent/controller race tests — passed.
- Opaque Go HTTP 400 classification regression — passed.
- `git diff --check` — passed.

根模块、Desktop 独立模块、lint、涉及请求头/会话身份/额度/恢复的 race 回归均
通过。没有新增界面功能的原生 UI 验证结论，也没有执行提交、推送或发布。

Raw sanitized local logs are in `/tmp/reasonix-live-multiprovider/`; preceding
DeepSeek logs are in `/tmp/reasonix-live-official/`. They are ephemeral local
evidence, not published artifacts. No request/response reasoning bodies or
credentials are included in this report.

Credential scan checked 295 modified/untracked source and local evidence files
against all four exact supplied keys: zero matches. The in-memory credential
supervisor was then stopped and temporary test binaries removed; sanitized logs
remain for diagnosis. / 四把密钥的精确值扫描覆盖 295 个源码和证据文件，零匹配；
随后退出持有密钥的测试进程并清理测试二进制，保留脱敏日志。
