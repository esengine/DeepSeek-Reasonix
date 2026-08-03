# Active Tool Result Projection

Active Tool Result Projection is an opt-in cache experiment:

```toml
[agent]
tool_result_projection = true
```

It can also be enabled for one headless run with `reasonix run --tool-result-projection ...`.

At the configured stale-result threshold, Reasonix archives the full result locally and rewrites only results before the protected recent/active-turn tail. The provider-visible projection is deterministic: tool name, original byte count, a short SHA-256 identity, and bounded head/tail content. It contains no archive path or timestamp. System prompts, tool schemas, tool order, tool-call pairs, assistant reasoning, errors covered by `keep`, and the active tail stay unchanged.

The switch defaults to `false` to preserve the historical snip/prune baseline. Usage metrics expose `tool_results_projected` and `projection_saved_chars` on the next provider request, where the projected history actually participates. Use `e2ebench -mode harness-ab` with `baseline` versus `projection` (or `delivery` versus `delivery-projection`) profiles to compare task success, cache hit rate, tokens, and cost.
