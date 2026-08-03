# Harness A/B experiments

`e2ebench -mode harness-ab` runs a paired, resumable experiment over the committed end-to-end task suite. It is intended for comparing two Reasonix builds or prompt profiles while holding the model, tasks, deadlines, grader, retry policy, and per-arm token budget constant.

## Run an experiment

Build or otherwise provide the two binaries first, then use a new persistent directory for the experiment:

```bash
go run ./cmd/e2ebench \
  -mode harness-ab \
  -suite benchmarks/e2e \
  -model e2e \
  -environment-id deepseek-v4-flash-2026-08-03 \
  -baseline-bin /path/to/reasonix-baseline \
  -candidate-bin /path/to/reasonix-candidate \
  -baseline-profile baseline \
  -candidate-profile projection \
  -repetitions 3 \
  -infra-retries 1 \
  -budget 400000 \
  -run-dir .reasonix-bench/cache-projection-001
```

The token budget is applied independently to each arm. Task order is stable, while which arm runs first alternates across task/repetition cells to reduce ordering bias. `-environment-id` is a non-secret, user-defined label for the provider route, resolved model, pricing revision, and other external conditions that the harness cannot safely derive from credentials; use a new run directory if those conditions change.

Re-run the exact command with the same `-run-dir` to resume. Completed cells are not admitted again. Each provider admission is logged before the process starts; after a crash, an admission without a durable result is closed as `infra_failed` and follows the frozen infrastructure-retry policy. Run only one harness process against a run directory at a time.

If the suite, either binary, model, profile, repetitions, retry policy, or budget changed, the harness refuses to reuse the frozen run directory; start a new experiment instead.

## Artifacts

The run directory contains:

- `manifest.json`: schema-versioned harness protocol and experiment identity, privacy-safe binary/suite labels and SHA-256 digests, task digests, model/environment labels, profiles, deadlines, repetitions, retry policy, and budget. Resolved absolute paths remain process-local.
- `attempts.jsonl`: append-only, fsynced write-ahead log. An actual attempt has an `admission_started` event followed by `attempt_finished`. A torn final line is removed on resume; earlier corruption is an error.
- `results.json`: machine-readable projection of latest cells, arm totals, and paired statistics.
- `results.csv`: one row per task/repetition pair for external analysis.
- `report.md`: continuously refreshed human-readable summary.

The artifacts contain no provider credentials or resolved local paths; common infrastructure errors are path-sanitized before persistence. Treat task IDs, user-supplied labels, and benchmark outputs according to the repository's normal privacy policy.

## Scoring and retries

Outcomes use a fixed taxonomy: `passed`, `verification_failed`, `agent_error`, `timeout`, `suite_budget_exhausted`, and `infra_failed`.

Only `infra_failed` is retried automatically. Agent errors, failed verification, timeouts, and budget exhaustion are terminal scored outcomes. A terminal infrastructure failure remains visible but is excluded from accuracy and paired statistics. Tokens and cost from every actual attempt with available metrics, including infrastructure retries, remain in the arm totals. The report exposes metric gaps; totals are lower bounds when a killed process could not flush its metrics file.

The paired report includes the four pass/fail cells, candidate-minus-baseline percentage-point delta, and an exact two-sided McNemar p-value based only on discordant eligible pairs. A small task suite has low statistical power; do not treat a positive point estimate alone as evidence of a general improvement.

## Cache interpretation

Cache hit rate is cached prompt tokens divided by cache-hit plus cache-miss tokens reported by `reasonix run --metrics`. Harness A/B does not change provider-visible prompts itself. For cache experiments, keep the provider/model route and stable prefix identical, and change only the component under test.

Profiles are `baseline`, `delivery`, `projection`, and `delivery-projection`. Projection profiles apply a process-local experiment override and do not modify persisted user configuration.
