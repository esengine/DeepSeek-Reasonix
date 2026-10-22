---
name: video-production
description: "Plan, generate, download, check, revise and deliver a video through the tools this session has: brief, script, storyboard, generation jobs, assembly, acceptance, delivery, kept in a versioned workspace manifest so work resumes after a restart and a revision redoes only the affected shots. Use when the user asks to make, edit, extend or fix a video, or to resume one."
runAs: inline
---

# Video production

This skill is **inlined**. The host owns execution, permissions, cancellation
and recovery; this workflow owns the creative plan and the manifest that
records it. Never claim a capability you have not discovered.

## 0. Discover before promising

Search capabilities before committing to a plan:

- generation: `use_capability` search for "generate video" / "image to video";
- download: `tool:download_artifact` (present only when an MCP server is configured);
- technical acceptance: `tool:check_video` (present only when ffmpeg is installed);
- frame review: `tool:read_video`; assembly: `bash` with ffmpeg when it is installed.

Tell the user which steps have no tool. A missing capability blocks only the
steps that depend on it; plan the rest.

## 1. The manifest is the state

Keep one manifest per production at `video/<production_id>/manifest.json`,
a JSON object with these fields:

- `version` (1), `production_id`, `revision`, `script_revision`;
- `brief`: `revision`, duration, aspect_ratio, width, height, audio, budget, `required_reviews`;
- `shots[]`: `shot_id`, `start`, `end`, `prompt_revision`, `prompt`, `references`, `depends_on`;
- `jobs[]`: `shot_id`, `tool`, `job_id`, `task_id`, `idempotency_key`, `state`, `result_ref`;
- `artifacts[]`: `artifact_id`, `version`, `shot_id`, `path`, `receipt`, `sha256`, `bytes`, `superseded_by`;
- `reviews[]`: `artifact_id`, `version`, `sha256`, `brief_revision`, `scope`, `ranges`, `reviewer`, `verdict` (pass|fail|unverified|not_applicable), `evidence`;
- `delivery`: `artifact_id`, `version`, `limitations`, `provisional`, `user_accepted`.

`jobs[].state` is one of planned, submission_unknown, generating, generated,
downloaded, review_pending, rework_required, accepted, delivered. Rules:

- Read it first on every turn of the production. If it exists, you are resuming.
- `version` must be `1`. On any other version, stop and tell the user; do not rewrite it.
- Preserve every field you do not understand, exactly, on each edit.
- Bump `revision` on each write. Write it **before** any paid or long-running
  submission and **after** each state change.
- Script-local variables and your own memory are not state; the manifest is.

## 2. Workflow

1. **Brief** — duration, aspect ratio, resolution, audio/subtitles, style,
   budget (money and retries), required review scopes. Ask only what blocks.
2. **Script** — record `script_revision`.
3. **Storyboard** — stable `shot_id`s (`s01`, `s02`…), time ranges, prompts,
   references, dependencies. Show it and get approval before paid generation.
4. **Generate** — per shot: set the job to `planned`, write the manifest, then
   submit. Record the host job id and remote task id exactly as the tool
   reported them. A long task continuing in the background is not a failure:
   collect it with `wait` / `bash_output`.
5. **Download** — `download_artifact` with the `tool-result:` reference and the
   exact JSON pointer of the URL field. Never copy URLs out of prose. Record
   path, `sha256`, bytes and the receipt path as a new artifact version.
6. **Check** — `check_video` with the brief's duration, size, aspect and audio
   policy and the artifact's `expected_sha256`. Record the report.
7. **Assemble** — concatenate accepted shot versions; the output is a new
   artifact with its own version and checks.
8. **Accept** — see §3. **Deliver** — name the final artifact version, its
   checks, and any limitation the user accepted.

## 3. Evidence, not claims

- `generated` needs a task result; `downloaded` needs a file whose hash you
  recorded; `accepted` needs a passing review for every required scope.
- `check_video` proves technical facts only. Its `unchecked` scopes (content,
  motion, audio and sync) need a reviewer that can perceive them; frames from
  `read_video` cover only what is visible in the sampled frames.
- Without such a reviewer, record the scope as `unverified` with the missing
  capability. Do not mark the delivery accepted; offer a provisional delivery
  that lists what is unverified, and record the user's acceptance of it.
- A review binds to the artifact hash and the requirement revision. New bytes
  or new requirements make the affected reviews stale.

## 4. Resume and never resubmit blindly

- On resume, read the manifest, then query every job that is not terminal
  (`wait`, `bash_output`) before submitting anything.
- A job whose submission outcome is unknown is `submission_unknown`. Do not
  submit it again unless the service offers an idempotency key you recorded or
  the user explicitly approves a possibly duplicate (billable) submission.
- A shot already `downloaded` resumes at checking, never at generation.

## 5. Revisions

For "make shot 2 a night scene":

1. resolve the stable `shot_id`; leave every other shot and version untouched;
2. add a new prompt revision and a new artifact version for that shot only;
   keep superseded versions in `artifacts`;
3. mark that shot's reviews and the assembly's reviews stale;
4. regenerate, download, check that shot; reassemble; recheck the assembly.

Stay within the brief's retry budget; when it is spent, stop and ask.
