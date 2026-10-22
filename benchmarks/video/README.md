---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-19
---

# Video production suite / 视频制作评测集

Offline, deterministic tasks for the `video-production` workflow. The video
service is `cmd/fakevideomcp`, so no paid service runs; the model is whatever
the run is configured with, so a run still spends model tokens.

离线确定性任务，视频服务由 `cmd/fakevideomcp` 模拟，不调用付费视频服务；模型按运行配置调用，
仍会消耗模型 token。

## Setup / 准备

1. Start the service and keep its log for the verifiers:

   ```bash
   go run ./cmd/fakevideomcp -addr 127.0.0.1:7591 -log /tmp/fakevideomcp.jsonl
   ```

2. Declare it in the user config (`~/.reasonix/config.toml`), so no task
   workdir has to be approved as a project server:

   ```toml
   [[plugins]]
   name = "video"
   type = "http"
   url = "http://127.0.0.1:7591/mcp"
   load = "always"
   ```

3. Install ffmpeg for `check_video`; without it the tool is absent and the
   workflow records technical review as unavailable.
4. Run the suite:

   ```bash
   FAKEVIDEO_LOG=/tmp/fakevideomcp.jsonl go run ./cmd/e2ebench -suite benchmarks/video
   ```

Truncate the log between runs: verifiers count every submission for their
production id in it.

运行前启动模拟服务、在用户配置中声明、可选安装 ffmpeg，并设置 `FAKEVIDEO_LOG`；两次运行之间清空日志。

## Tasks / 任务

| Task | Seed | Pass condition |
| --- | --- | --- |
| `video-fresh-3shot` | empty | manifest v1 with 3 shots; each current artifact exists, hash matches, has a receipt; exactly one submission per shot |
| `video-revise-shot` | 3 downloaded shots | s01/s03 bytes unchanged; s02 current version is 2, v1 kept; one submission, for s02; unknown field kept |
| `video-resume-after-download` | s01 downloaded, s02 planned | both shots downloaded; only s02 submitted |
| `video-submission-unknown` | s02 `submission_unknown` | s01 gets a technical review record; s02 not resubmitted and its state not advanced without a fact |
| `video-missing-reviewer` | empty | the shot is downloaded; `audio_and_sync` review is `unverified`; delivery not a full acceptance |

## Metrics / 指标

Each metric names what it counts, from which anchor, over which population, and what it excludes.

| Metric | Counts | Anchor | Population | Excludes |
| --- | --- | --- | --- | --- |
| Accepted deliveries | tasks whose verifier passed | `verify.sh` exit status | tasks attempted in the run | tasks that timed out before the verifier ran |
| Duplicate submissions | log entries beyond one per (production, shot) | `FAKEVIDEO_LOG` | all tasks in the run | revisions the task asked for |
| Model turns, latency, cost | as e2ebench reports them | provider request boundary (`-meter`) | measured requests | requests without provider usage (reported unknown, never zero) |

Prefix byte equality is a structural guard and not a measured cache-hit rate.
A required scope left `unverified` is never counted as an accepted delivery.

前缀字节一致只是结构保证，不等于实测缓存命中率；必需范围未验证不计入已验收交付。
