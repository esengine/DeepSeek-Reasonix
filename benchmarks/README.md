# Reasonix Benchmarks

Two primary harnesses live under `benchmarks/`; `cmd/e2ebench` also exposes a
SWE-bench Verified mode:

- `e2e/` — the committed end-to-end task suite, driven by
  [`cmd/e2ebench`](../cmd/e2ebench/main.go). It runs each task against a real
  provider and emits a markdown + JSON report (accuracy, cache-hit rate, token
  use, cost) suitable for pasting into a PR.
- `context-maintenance-e2e/` — a standalone seed → resume → comprehension
  harness that A/B-compares cold-restart cache behavior with and without
  context pruning.

## Directory layout

```text
benchmarks/
├── e2e/
│   └── tasks/                     # one dir per task: task.toml + verify.sh + workdir/ seed
├── swebench/
│   ├── select_subset.py         # helper for choosing evaluation instances
│   └── subset.json               # committed SWE-bench Verified subset
└── context-maintenance-e2e/
    ├── main.go
    └── run/                       # state dir written by seed/resume (default)
```

## Task corpus stratification

The suite is stratified by real coding-agent workload classes, not toy-task
convenience — the classes are what the per-class compare tables and marginal-
utility readouts key on. Current coverage vs. target:

| Class | Target | Committed | Notes |
| --- | ---: | ---: | --- |
| `atomic-bugfix` | 8 | 8 | short anchored fixes; routes ExecutorOnly by design |
| `repo-exploration` | 6 | 6 | multi-file reading, invented-token answers so they can't be guessed |
| `multi-file-bugfix` | 8 | 8 | one bug spanning ≥2 files; naturally engages the planner gate |
| `refactor` | 6 | 6 | behavior-preserving restructuring, structure asserted |
| `failing-test-diagnosis` | 6 | 6 | unittest suite red → fix source; tests checksummed |
| `api-integration` | 4 | 4 | use a provided local package per its README |
| `ambiguous` | 4 | 4 | underspecified ask; grader accepts the defensible core |
| `long-horizon` | 4 | 4 | multi-requirement specs; planner-depth full |
| `codegen` / `delegation` | — | 3 | legacy smoke tasks (fizzbuzz, palindrome, subagent-delegation) |

Grader authoring rule: every task must fail `verify.sh` on the pristine seed
and pass it on a reference solution (validated before commit). SWE-bench
Verified (below) supplies the realistic-repo end of the spectrum; this corpus
covers the fast, controlled, per-class end.

Each task under `e2e/tasks/<id>/` contains:

| File | Purpose |
| --- | --- |
| `task.toml` | The task definition (prompt, step/timeout limits). |
| `verify.sh` | The grader: exits 0 iff the agent's artifacts are correct. |
| `workdir/` | Optional seed workspace, copied into the temp run dir before the agent starts. |

## task.toml schema

`e2ebench` reads `benchmarks/e2e/tasks/<id>/task.toml` with the BurntSushi TOML
decoder. The task ID is the directory name; tasks run in sorted ID order.

| Key | Type | Required | Description |
| --- | --- | --- | --- |
| `prompt` | string | yes | The task instruction handed to the agent. |
| `class` | string | no | Task class label (e.g. `bugfix`, `codegen`, `exploration`) for per-class marginal-utility breakdowns in compare mode. |
| `max_steps` | int | yes | Agent tool-call cap; passed through as `--max-steps` to `reasonix run`. |
| `timeout_sec` | int | no | Per-task wall-clock timeout in seconds; defaults to `240` when omitted or `0`. |

Example (`tasks/fizzbuzz/task.toml`):

```toml
prompt = "Create a file named fizzbuzz.py containing a function fizzbuzz(n) that returns the string 'Fizz' when n is divisible by 3, 'Buzz' when divisible by 5, 'FizzBuzz' when divisible by both 3 and 5, and otherwise the number as a string. Do not print anything at import time."
max_steps = 12
timeout_sec = 180
```

## verify.sh contract

`verify.sh` is the grader for a task:

- It is a `bash` script run with `set -e`; exit code `0` means the task passed.
- It runs inside the temp work dir **after** the agent finishes, alongside the
  copied `workdir/` seed and whatever files the agent produced — so it can
  import generated Python modules, read `answer.txt`/`result.txt`, etc.
- The harness copies `verify.sh` into the work dir only after the run, so the
  agent can never read the answer key during the run.
- Its stdout/stderr is streamed to the job log (stderr), not the report.

Examples: `compaction/verify.sh` normalizes `answer.txt` (strip whitespace,
lowercase) and compares it to the expected `aldermoor-verrin`;
`fizzbuzz/verify.sh` imports the generated module and asserts on
`fizzbuzz(3)`, `fizzbuzz(5)`, `fizzbuzz(15)`, `fizzbuzz(7)`.

Python graders must start with
`export PYTHONPYCACHEPREFIX="$(mktemp -d)"`: macOS system Python caches
bytecode centrally keyed by absolute path, so an agent edit that keeps a
file's size within the same mtime second would otherwise execute stale
bytecode while tracebacks display the new source.

## Running the e2e suite

Prerequisites: a `reasonix` binary (or `go run ./cmd/reasonix` …) with a
configured provider. The harness invokes the agent as
`reasonix run --auto --metrics <path> [--model NAME] [--max-steps N] [--profile delivery] [--ablate ARM] <prompt>`
inside a temp copy of the task's `workdir/`; the `--auto` flag is deliberate so
unattended fixture writes are allowed.

```sh
# Run the committed suite, report to stdout
go run ./cmd/e2ebench

# Same suite with the delivery prompt profile
go run ./cmd/e2ebench -profile delivery

# Write the markdown report to a file and the raw results to JSON
go run ./cmd/e2ebench -out report.md -json report.json

# Grade a PR's diff (generates tests for the diff, grades with the repo's tests)
go run ./cmd/e2ebench -mode diff -base origin/main-v2 -repo . -attempts 3 -timeout 1800
```

The markdown report contains the solved count, cost/tokens per solved task,
median wall time, cache-hit rate, and a per-task table with failure class
(`solved`, `timeout`, `wrong_patch`, `no_metrics`, `skipped`, or the agent's
own outcome).

### Flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `-mode` | `suite` | `suite` \| `diff` \| `swebench` \| `compare` \| `traj` (`diff` generates tests for the PR diff; `swebench` runs the official per-instance evaluation; `compare` renders KPI/Pareto readouts from 2+ `-json` reports; `traj` re-digests recorded trajectory files without spending tokens). |
| `-suite` | `benchmarks/e2e` | Suite root (must contain `tasks/<id>/`). |
| `-task` | *(all)* | Suite mode: run only these comma-separated task IDs (e.g. `-task fix-add-bug`); unknown IDs fail with the available list. |
| `-attempts` | `1` | Suite and diff modes: retry a task until an attempt passes, up to N; enables the `Pass@≤N` KPI, and TTCS charges a retried solve with its failed attempts' wall. |
| `-bin` | `reasonix` | Path to the reasonix binary. |
| `-model` | *(config default)* | Provider/model name. |
| `-profile` | `baseline` | Tool-surface/runtime tier: `baseline` \| `economy` \| `balanced` \| `delivery`. All but `baseline` append `--profile <tier>` to the agent invocation; `baseline` passes no flag (byte-identical legacy control, behaviorally `balanced`). Economy starts with the core tool set and pays `connect_tool_source` rounds plus prefix resets to grow it — the report's Tool surface line prices that trade. |
| `-ablate` | *(none)* | Ablation arm: comma-separated subsystems to switch off — `evidence`, `planner`, `subagent`, `retrieval`, `compaction`; `none` \| `all`. |
| `-out` | *(stdout)* | Write the markdown report here. |
| `-json` | *(none)* | Write the JSON report here (optional). |
| `-trajectories` | *(none)* | Suite mode: write one `<task-id>.trajectory.jsonl` per task into this directory (the agent's full event stream with timestamps — see `reasonix run --trajectory`). The report gains a time-attribution line (tools vs. model) and each JSON result a `trajectory` digest. |
| `-force-planner` | `false` | Suite mode: prefix each prompt with a plan-first directive so the two-model turn engages regardless of the planner gate. Use for the "with planner" arm of an A/B; results carry `plan_forced` so arms are only comparable with equal forcing. |
| `-cache` | `cold` | Suite mode: `cold` runs each task as a fresh session (the fair cross-agent comparison arm); `warm` primes the provider prefix cache with a one-step run in the same workdir first, measuring the long-lived-session steady state. Never mix arms in one report — compare them with `-mode compare cold.json warm.json`. |
| `-budget` | `800000` | Abort once total tokens cross this (`0` = no cap). Remaining tasks are reported as skipped. |

Diff-mode flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-repo` | `.` | Repo root (diff mode). |
| `-base` | *(none)* | Base ref to diff the PR head against (diff mode). |
| `-test-cmd` | `go test` | Grader command run on the affected packages (diff mode). |
| `-max-steps` | `80` | Agent tool-call cap for the diff task. |
| `-timeout` | `1200` | Agent timeout in seconds (diff mode). |
| `-attempts` | `1` | Diff mode: retry up to N times until a run passes (stochastic agent). |

## Dataset retention

Keep every `-json` report and `-trajectories` directory from real runs: they
are the accumulating corpus — per-task contracts-to-be, full event
trajectories, checkpoint oracle verdicts, stop curves and phase traces — that
any future offline learning (routing, stop policies, budgets) would train
and evaluate on. The control plane stays deterministic and interpretable
until that corpus reaches a scale where learned policies can be judged
against the same oracles that produced it; nothing learned lands before it
beats the deterministic baseline on these numbers.

## A/B compare mode

Run the same suite twice and let the harness judge the trade:

```sh
go run ./cmd/e2ebench -force-planner -trajectories t-a -json with.json
go run ./cmd/e2ebench -ablate planner -trajectories t-b -json without.json
go run ./cmd/e2ebench -mode compare with.json without.json
```

Compare mode renders a per-solved delta table (solve rate, model requests,
planner requests, model rounds, tool calls, tokens, wall, cost), an overall
marginal-utility line (`accuracy +X.Xpp · wall/task +Y.Ys`), and — when tasks
carry `class` labels — a per-class breakdown, so a subsystem's uplift and
latency cost can be judged per task class instead of globally.

## SWE-bench Verified mode

`e2ebench` can also run the agent inside the official SWE-bench evaluation
images and hand the resulting patches to the official grader:

```sh
# Requires Docker, the `swebench` Python package, evaluation images, and a
# network/proxy setup that prevents the agent from reading upstream fixes.
go run ./cmd/e2ebench -mode swebench \
  -subset benchmarks/swebench/subset.json \
  -network reasonix-eval -proxy http://127.0.0.1:8080
```

SWE-bench mode accepts the `-model`, `-profile`, `-ablate`, `-permission`,
`-workers`, `-dataset`, `-run-id`, `-harness-python`, and `-keep-images` flags;
its report is produced by the official harness rather than the suite JSON
writer.

## Adding a new task

1. Create `benchmarks/e2e/tasks/<task-id>/`.
2. Write `task.toml` with `prompt`, `max_steps`, and `timeout_sec` (see
   [schema](#tasktoml-schema)).
3. If the task needs seed files, add them under `workdir/` (they are copied
   into the temp run dir; symlinks are skipped).
4. Write `verify.sh`: `set -e`, exit 0 iff the agent's artifacts are correct.
   Keep the expected answer out of the prompt and seed; the script runs in the
   work dir and may validate anything the agent produced.
5. Iterate on just that task with the single-task filter, then commit:

   ```sh
   go run ./cmd/e2ebench -task <task-id>
   ```

## context-maintenance-e2e

This harness measures what happens when a long session goes idle past the
provider's cache TTL and then resumes: it A/B-compares cold-restart miss tokens
with and without pruning, and checks that the agent re-reads a file behind a
prune placeholder instead of hallucinating.

It is hardcoded to the `deepseek-v4-flash` model at `https://api.deepseek.com`
and requires the `DEEPSEEK_API_KEY` environment variable.

```sh
export DEEPSEEK_API_KEY=...

# Seed both arms (pruned + control) with a large session and warm the cache
go run ./benchmarks/context-maintenance-e2e seed

# Wait past the provider's cache TTL, then resume: prune the "pruned" arm and
# compare cold-restart miss tokens
go run ./benchmarks/context-maintenance-e2e resume

# Run the comprehension trials (agent must re-read a pruned file and answer
# from it); exits non-zero unless every trial passes
go run ./benchmarks/context-maintenance-e2e comprehension
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-dir` | `benchmarks/context-maintenance-e2e/run` | State directory for `seed`/`resume` (sessions + `meta.json`, `resume-<ts>.json`). |
| `-trials` | `5` | Number of comprehension trials. |

## See also

- [`docs/CLI.md`](../docs/CLI.md) — the `reasonix run` flags the e2e harness
  passes through (`--auto`, `--metrics`, `--model`, `--max-steps`,
  `--profile`, `--ablate`).
- [`cmd/e2ebench/main.go`](../cmd/e2ebench/main.go) — suite runner and report
  renderer.
