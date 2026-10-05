# fanout-width

A one-task suite for measuring what running a fan-out's members at the same
time is worth, against a serialised arm where the same graph runs one member at
a time.

It lives outside `benchmarks/e2e` for the same reason `upstream-edge` does: the
prompt fixes the graph shape, which the shared corpus deliberately never does.
This measures what the concurrency bought *given* a fan-out, not how often a
model reaches for one.

The three research items share nothing, so nothing but the scheduler decides
whether they overlap. Serialising them needs no ablation flag — the session
concurrency ceiling already does it:

```sh
go run ./cmd/e2ebench -suite benchmarks/fanout-width -bin <reasonix> -json wide.json
# then, with max_subagent_concurrency = 1 in the arm's config:
go run ./cmd/e2ebench -suite benchmarks/fanout-width -bin <reasonix> -json serial.json
go run ./cmd/e2ebench -mode compare wide.json serial.json
```

Read the **Fan-out** section: the wide arm should report work well above wall
(members overlapped) and near-zero slot wait, the serial arm work equal to wall
and a slot wait that accounts for the difference. Both must still solve the
task — a concurrency win that costs accuracy is not a win.
