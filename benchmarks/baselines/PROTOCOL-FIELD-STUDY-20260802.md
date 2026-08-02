# Protocol Field Study — 2026-08-02 (isPalindrome task)

Real-world task comparison: implement `isPalindrome` (5 test cases, Unicode-aware),
run `go test ./...` until green. Same task, three protocol/model combinations,
run through Reasonix desktop sessions.

## Setup

| Session dir | Model | Protocol | Cache mechanism |
|---|---|---|---|
| `test/claude` | DeepSeek V4 Flash | Responses (stateless) | Context Caching on Disk (server-side) |
| `test/openai` | DeepSeek V4 Flash | Chat Completions (full history) | Context Caching on Disk (server-side) |
| `test/qwen` | Qwen 3.7-max (thinking) | Responses (stateless) | GPU Session cache, 5-min TTL |

All three passed `go test ./...` with a correct implementation.

## Results

| Metric | Responses (DS Flash) | Chat (DS Flash) | Responses (Qwen 3.7-max) |
|---|---|---|---|
| Wall-clock | **3m38s** | 5m35s | 12m44s |
| Requests | **34** | 38 | 46 |
| Total tokens | **1,296,318** | 1,616,328 | 1,876,897 |
| Cache hit (session avg) | 95.14% | **96.35%** | 90.46% |
| Cost | **¥0.1267** | ¥0.1469 | — (Token Plan) |
| Implementation lines | **25** | 82 | 42 |

## Headless per-round cache curve (DeepSeek Flash + Responses, events-jsonl)

8 model rounds from the same task run headless; shows cold start → steady-state:

```
Round 1: hit=     0 miss= 30881  命中=  0.0%   ← cold start
Round 2: hit= 30976 miss=   318  命中= 99.0%   ← prefix established
Round 3: hit= 31232 miss=   552  命中= 98.3%
Round 4: hit= 32000 miss=   154  命中= 99.5%
Round 5: hit= 32384 miss=   164  命中= 99.5%
Round 6: hit= 32768 miss=   288  命中= 99.1%
Round 7: hit= 33408 miss=    96  命中= 99.7%
Round 8: hit= 33664 miss=    51  命中= 99.8%
Cumulative: 87.4%  |  Steady-state per-round: 99%+
```

## Findings

1. **DeepSeek Responses is the best default**: 35% faster, 14% cheaper, 20% fewer
   tokens than Chat Completions on the same model — while sharing the same disk-cache
   layer (both protocols hit Context Caching on Disk).
2. **Chat Completions edges cache-hit rate (+1.2%)** because full-history requests
   produce a longer cacheable prefix, but the cost delta is negligible (~0.02% tokens).
3. **Qwen thinking models are 3.5x slower for mechanical tasks**: reasoning tokens
   are billed as output, never cacheable, and the 5-min GPU session cache expires
   more easily than DeepSeek's disk cache. Qwen 3.7-max took 12m44s / 46 requests /
   1.9M tokens vs DeepSeek Flash 3m38s / 34 requests / 1.3M tokens.
4. **All-zero usage records observed** (DashScope Round 2 in earlier bench): the
   `response.completed` usage object can exist with every field zero; see #7168's
   suppression gate.

## Related

- #7168 — Responses protocol compatibility + all-zero usage gate + vendor-aware cache TTL
- #7153 — baseline tooling (this directory)
