---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-19
---

# Video production manifest, version 1 / 视频制作清单 v1

The `video-production` skill keeps one manifest per production at
`video/<production_id>/manifest.json`. It is an ordinary workspace file the
model edits with the file tools; the host does not write it. A worked example
is [video_manifest.v1.json](fixtures/video_manifest.v1.json).

`video-production` 技能为每个制作维护一份 `video/<production_id>/manifest.json`。
它是普通工作区文件，由模型用文件工具编辑，宿主不自动写入。示例见上方链接。

## Rules / 规则

- `version` is `1`. A reader that meets another version stops and does not rewrite the file.
- Every field a writer does not understand is preserved byte-for-byte on edit.
- `revision` increases by one on each write.
- It is written before any paid or long-running submission and after each state change.
- Three identities stay distinct: host `job_id`, remote `task_id`, artifact `artifact_id` + `version`.

- `version` 为 `1`；遇到其他版本时停止，不改写文件。
- 编辑时原样保留不认识的字段；每次写入 `revision` 加一。
- 付费或长耗时提交前、每次状态变化后写入。
- 宿主 `job_id`、远端 `task_id`、产物 `artifact_id` + `version` 三者分开。

## Fields / 字段

| Field / 字段 | Contents / 内容 |
| --- | --- |
| `version`, `production_id`, `revision` | Format version, stable id, write counter / 格式版本、稳定 ID、写入计数 |
| `brief` | `revision`, duration, aspect, resolution, audio, subtitles, budget, `required_reviews` / 需求、预算、必需验收范围 |
| `script_revision` | Revision of the approved script / 已确认脚本版本 |
| `shots[]` | `shot_id`, `start`, `end`, `prompt_revision`, `prompt`, `references[]`, `depends_on[]` |
| `jobs[]` | `shot_id`, `tool`, `job_id`, `task_id`, `idempotency_key`, `state`, `result_ref` |
| `artifacts[]` | `artifact_id`, `version`, `shot_id`, `path`, `receipt`, `sha256`, `bytes`, `superseded_by` |
| `reviews[]` | `artifact_id`, `version`, `sha256`, `brief_revision`, `scope`, `ranges`, `reviewer`, `verdict`, `evidence` |
| `delivery` | `artifact_id`, `version`, `limitations[]`, `provisional`, `user_accepted` |

## States / 状态

`jobs[].state` records remote execution; shot progress follows from the
artifacts and reviews that exist, never from a state the writer asserts.

| State / 状态 | Required fact / 必需事实 |
| --- | --- |
| `planned` | Written before submission / 提交前写入 |
| `submission_unknown` | The call returned no task or result; never resubmitted blindly / 无结果，不盲目重提 |
| `generating` | A `task_id` or background `job_id` exists / 已有远端或后台任务 ID |
| `generated` | A `result_ref` (`tool-result:`) exists / 已有结果引用 |
| `downloaded` | An artifact version with `sha256` exists / 已有带摘要的产物版本 |
| `review_pending`, `rework_required` | Reviews missing or failing / 验收缺失或失败 |
| `accepted` | Every `required_reviews` scope passes for this hash and brief revision / 必需范围全部通过 |
| `delivered` | `delivery` names this artifact version / 交付记录指向该版本 |

`reviews[].verdict` is `pass`, `fail`, `unverified`, or `not_applicable`.
`unverified` records the missing capability. A review whose `sha256` or
`brief_revision` no longer matches is stale and does not count.

`reviews[].verdict` 取 `pass`、`fail`、`unverified` 或 `not_applicable`；`unverified` 记录缺失能力。
摘要或需求版本不一致的验收视为过期，不计入。
