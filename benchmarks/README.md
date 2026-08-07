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
│   └── tasks/
│       ├── compaction/            # task.toml + verify.sh + workdir/ seed
│       ├── fix-add-bug/
│       ├── fizzbuzz/
│       ├── palindrome/
│       └── subagent-delegation/
├── swebench/
│   ├── select_subset.py         # helper for choosing evaluation instances
│   └── subset.json               # committed SWE-bench Verified subset
└── context-maintenance-e2e/
    ├── main.go
    └── run/                       # state dir written by seed/resume (default)
```

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
| `-mode` | `suite` | `suite` \| `diff` \| `swebench` (`diff` generates tests for the PR diff; `swebench` runs the official per-instance evaluation) |
| `-suite` | `benchmarks/e2e` | Suite root (must contain `tasks/<id>/`). |
| `-bin` | `reasonix` | Path to the reasonix binary. |
| `-model` | *(config default)* | Provider/model name. |
| `-profile` | `baseline` | Prompt profile: `baseline` \| `delivery`. `delivery` appends `--profile delivery` to the agent invocation. |
| `-ablate` | *(none)* | Ablation arm: comma-separated subsystems to switch off — `evidence`, `planner`, `subagent`, `retrieval`, `compaction`; `none` \| `all`. |
| `-out` | *(stdout)* | Write the markdown report here. |
| `-json` | *(none)* | Write the JSON report here (optional). |
| `-trajectories` | *(none)* | Suite mode: write one `<task-id>.trajectory.jsonl` per task into this directory (the agent's full event stream with timestamps — see `reasonix run --trajectory`). The report gains a time-attribution line (tools vs. model) and each JSON result a `trajectory` digest. |
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
5. Run the suite. Since `e2ebench` has no single-task filter, iterate against a
   scratch suite root:

   ```sh
   mkdir -p scratch/tasks/<task-id>
   cp -r benchmarks/e2e/tasks/<task-id>/* scratch/tasks/<task-id>/
   go run ./cmd/e2ebench -suite scratch
   ```

   When it passes, remove the scratch dir and commit the task.

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
