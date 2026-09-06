# OpenCode comparison after live tests / 真实测试后的 OpenCode 对照

Date / 日期: 2026-09-05. Source baseline / 源码基线:
[`bbd72fb8b0bb6de580d2041a0150016227c63ac0`](https://github.com/anomalyco/opencode/tree/bbd72fb8b0bb6de580d2041a0150016227c63ac0).
This is a source comparison with Reasonix live experiments, not a same-account
end-to-end benchmark of the OpenCode executable. / 本文结合 OpenCode 固定源码与
Reasonix 实测；没有宣称运行 OpenCode 完整客户端作同账户对照。

## 1. Normal text completion without the requested tool / 正常结束却没有执行工具

Kimi K3 returned HTTP 200 and `stop` with no tool-call fields on the original
wire. It sometimes invented an echo result. Reasonix did not drop a received
call: the call was absent before parsing. Both low and max effort reproduced
this behavior with the minimal no-argument echo fixture.

Kimi 的问题发生在模型输出层：原始响应没有工具调用，却输出了声称的结果；
low、max 两个档位均出现过。不能把调高思考档位当成已证实的修复。

OpenCode's [session loop](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/prompt.ts#L1096)
exits on a normal finish without tool parts. It explicitly continues when real
tool parts exist even if the provider says `stop`. It does not generally infer
an unperformed task from prose and automatically ask again.
Its [Kimi prompt selection](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/system.ts#L45)
and [action instructions](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/prompt/kimi.txt#L3)
make execution requirements explicit. Ordinary runs do not force tools:
[`required` is selected for JSON-schema structured output](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/prompt.ts#L1285).

OpenCode 也不会因为用户的自然语言要求而普遍强制工具调用。应吸收它的明确
行动提示与真实工具状态判断，而不是把 `stop` 改为重试，或给所有轮次设置
`tool_choice=required`，从而破坏问答、最终总结和权限边界。

Prompt-only experiment: adding a short execution-and-evidence instruction at
max effort completed 6/6 baseline/cut samples, versus 5/6 with the original
minimal prompt. Production prompts are unchanged. The samples were sequential,
small, and not statistically conclusive. A separate three-sample Kimi parameter
fixture passed twice; one run used a label that did not preserve the requested
Unicode/newline value. All three runs called each tool once. This semantic value
mismatch is not malformed JSON and is not solved by SDK tool-name repair.

短提示实验六例全完成，可作为优先验证的候选；不能直接宣称问题已经修复。
参数值不符应由工具返回具体校验错误，允许模型在授权范围内纠正；适配器不应
擅自替换业务参数。首个错误样本没有记录原始参数字节，故没有进一步断言其
精确错误值；三例工具各调用一次，也不能证明参数纠错循环已经验证。

Recommended change / 建议：

- First validate a small, static instruction that action requests require real
  tool execution and that completion claims require actual results. Evaluate
  realistic read/edit/test tasks as well as the echo fixture. / 先验证短小、
  稳定的行动提示，覆盖真实读取、编辑、测试任务；不能只优化一个 echo 用例。
- If a host-owned workflow already has a verifiable unfinished required action,
  allow at most one bounded reminder, sharing the existing turn budget. A
  completed write, permission rejection, cancellation or unknown side effect
  must prevent action replay. Ordinary conversation should retain normal stops.
  / 仅在宿主已有确定的未完成动作状态时考虑一次提醒，共享预算；不得凭文字
  猜任务，也不得重放已完成写入、拒绝权限或结果未知的操作。
- This reminder would be a Reasonix extension, not behavior proven in OpenCode.
  Any system-prompt change changes the cache prefix once; do not inject changing
  reminders into healthy turns. / 自动提醒属于 Reasonix 的额外设计。固定提示
  修改会使对应缓存前缀变化一次，不应向正常轮次持续注入动态提示。

## 2. Opaque HTTP 400 on custom DeepSeek Messages / 自定义 Messages 的不透明 400

Six Go Flash/Pro replay-rejection probes deliberately damaged the second
outbound request while retaining the completed local tool result. Chat and
Responses returned an identifiable error and recovered: four probes each made
three requests and executed the tool once. Messages returned only
`{"model":"deepseek-v4-flash"}` or the Pro counterpart with HTTP 400. Both
Messages probes stopped after two requests, retaining the single completed tool.

Go 的 Chat、Responses 四例能识别并修复；Messages 两例缺少具体错误原因。
我们知道测试故意破坏了回放，但真实客户端没有代理的内部知识，因此不能由
HTTP 400 加模型名称推断 reasoning 缺失。

OpenCode's [retry classifier](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/retry.ts#L85)
also requires retryable status/metadata or recognized error text; this bare 400
body does not satisfy those conditions. Its current gateway
[requires matching wire formats](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/console/app/src/routes/zen/util/handler.ts#L214),
and the [same-format converter is an identity](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/console/app/src/routes/zen/util/provider/provider.ts#L215).
This source snapshot does not establish which deployed upstream or intermediary
removed the details. / 当前公开源码不能证明是哪一层部署丢失了错误详情，不能
直接把现场现象归因于某个转换函数。

Recommended change / 建议：

- Continue automatic repair for an explicit replay error or locally provable
  invalid history, using the existing one-time repair budget. / 明确回放错误、
  本地可证明无效的历史继续走现有一次修复路径。
- Preserve safe status, error code and available request identifiers; surface
  an explicit “upstream did not provide the cause” diagnostic for opaque errors.
  Do not retry all 400s or fabricate missing thinking/signatures. / 不透明错误
  清楚说明上游未提供原因；不把全部 400 改成自动重生成，不伪造证明。
- The service owner should preserve structured errors and retry headers on
  failure paths. The audited gateway keeps only content-type/cache-control from
  upstream headers on this path; locally generated quota errors have a separate
  path. No upstream issue/comment was posted. / 应由服务端保留错误和重试提示；
  本次没有代用户向上游提交 issue 或评论。
- Keep the recommended DeepSeek Chat route, while respecting explicit custom
  Messages/Responses selections. Provide a deliberate recovery-from-valid-history
  action if automatic classification is impossible, preserving completed results.
  / 保留推荐的 Chat 默认路由，自定义协议不静默切换；无法自动判断时，可提供
  明确的有效历史恢复入口，保留已完成结果，不重放未知写入。

## 3. Search succeeded but structured sources were absent / 搜索成功但来源缺失

Two Go Responses samples completed native search and the following echo call.
The raw search action contained queries but no sources; the final response's
message annotations were empty. This is a source-availability gap, not a failed
search request or demonstrated parser loss. Go's DeepSeek Messages search
sample supplied ten structured sources.

OpenCode's [independent websearch tool](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/tool/websearch.ts#L65)
uses Exa or Parallel through a separate MCP-style call, independent of the main
model's inference protocol. It returns actual tool output and asks for the
search permission. It does not establish that native Responses must supply
`action.sources`. / OpenCode 通过独立搜索服务提供工具结果，主模型用哪种协议
与搜索后端解耦；不能据此声称原生 Responses 一定能返回来源。

Recommended change / 建议：

- Reuse Reasonix's existing independent `web_search` owner. Keep the main model,
  protocol and thinking settings unchanged. / 复用已实现的独立搜索，不再建平行系统。
- Distinguish search execution, raw item replay, and structured source availability
  in tests and presentation. Missing sources are not a transport retry signal.
  / 分开验证搜索执行、原始 item 回放与来源可用性；缺来源不触发网络重试。
- Use the configured source-capable search route. Any fallback requires an
  already configured/authorized route and a finite auxiliary budget; do not send
  a vendor credential to another service. Never infer verified sources from
  generated prose. / 使用已配置且能提供来源的后端；后备搜索共享辅助预算，
  不跨供应商发送密钥，也不把模型生成的 URL 当作已验证来源。

## 4. Protocol-specific compatibility and retry ownership / 分协议兼容与重试归属

OpenCode [normalizes interleaved Chat reasoning, including empty values](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/provider/transform.ts#L322).
Its [Anthropic normalization preserves signed empty blocks](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/provider/transform.ts#L168).
These transformations should not be copied as a universal empty-reasoning rule.
Reasonix should retain its protocol contracts: Chat empty-field compatibility;
actual unsigned thinking for DeepSeek Messages; original proofs for native
Claude; complete raw items for Responses. / 不应跨协议照搬补空值。此前实现的
能力判定与原始回放资料仍应保留。

OpenCode [disables SDK retries by default and repairs malformed tool calls at the tool boundary](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/llm.ts#L296).
The pinned [session retry policy](https://github.com/anomalyco/opencode/blob/bbd72fb8b0bb6de580d2041a0150016227c63ac0/packages/opencode/src/session/retry.ts#L26)
uses five retries, exponential delay and jitter. Reasonix already has centralized
request ownership; retain its agreed Pi-style three fast retries and explicitly
selected main-session waiting semantics. Do not silently replace them with this
OpenCode version's constants. / 借鉴重试归属与错误分类，不照抄次数，也不声称
Reasonix 的主会话持续等待是 OpenCode 的默认行为。

## Delivery and next priority / 交付与下一步优先级

Already implemented during testing: correct Go client/session headers across
all three adapters and quota-aware errors that distinguish allowance exhaustion
from invalid credentials. Root/desktop suites, lint and focused race tests passed.
This analysis introduces no silent model/protocol switch or new global retry.

本轮已经落地三协议请求头与额度分类修复。剩余优先级：验证行动提示的真实任务
收益；补齐不透明上游错误的诊断/明确恢复入口；完善搜索来源可用性状态。
所有动作继续复用当前恢复预算、工具持久化和独立搜索实现。

See [live results and limitations](MULTIPROVIDER_VALIDATION.md). / 具体样本、
未通过场景及验证边界见实测报告，不能宣布 #9808 的任意端点场景已完全解决。
