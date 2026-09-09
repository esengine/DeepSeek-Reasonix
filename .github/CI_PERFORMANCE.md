# CI and release execution

[中文](CI_PERFORMANCE.zh-CN.md)

## Desktop PR checks

`desktop-prepare` generates Wails bindings and builds the Linux frontend once.
Its artifact is consumed by independent `desktop-frontend`, `desktop-browser`,
and `desktop-go` jobs. The existing required `desktop` check aggregates all
four results, rejects failures/cancellations/unexpected skips, and accepts a
path-based skip only when the changes detector succeeds. The other native OS
checks remain separate.

`node desktop/frontend/scripts/run-ci-tests.mjs --list` prints the unit test
plan. It expands the existing dedicated scripts and lifecycle hooks, discovers
new tests, and schedules each TypeScript suite once with its original loader.
Unsupported script syntax and conflicting explicit invocations fail closed.
CI runs two isolated processes at a time; the history performance benchmark
runs alone after them. Local dedicated `pnpm test:*` commands remain available.

## Memory screening

Protocol v2 still requires three independent processes, each completing
128 full, 128 windowed, 128 safety and 512 mixed round trips, with the same
checkpoints, five heap snapshots, GC, frame settling and screening thresholds.
Only the explicit mock memory-soak URL removes the fixture's artificial
1.5-second hydration latency. Hydration still crosses an asynchronous timer
task. Default browser and native geometry fixtures retain the delayed path.

The pointer rests outside topic rows and the warmed baseline follows a complete
round trip after layout switching, avoiding samples of temporary menu state.
Reports declare the protocol, and aggregation rejects older protocols.
`timings.json` records host-side counts, total time and maximum time for
navigation, frame settling, GC and heap capture/analysis. Short profiling runs
are diagnostic only and cannot pass the complete screening gate. A green gate
still does not prove offline heap-retainer attribution.

## Signed release artifacts

The successful stable SignPath preflight hands its full native matrix to the
desktop publisher. The publisher revalidates authorization, candidate identity,
signing contract and cache/docs guards, then verifies and publishes the same
signed bytes without rebuilding or signing them again. CLI and npm publication
still wait for the entire preflight; no public surface starts early.

Each platform bundle binds file sizes and SHA-256 hashes to candidate/control
SHAs, version, tag, channel, signing fingerprint, run, invocation and producer
attempt. Missing/extra platforms, conflicting identities, symlinks, duplicate
filenames or modified bytes stop publication. These transport hashes supplement
the existing Authenticode/minisign checks; they are not signature verification.

A failed-job retry may reuse earlier successful platforms from the same run
and invocation. A rebuilt platform replaces only its own fully verified bundle.
New workflow runs prepare a new artifact set. Standalone recovery still builds
and validates its own full matrix. pnpm dependencies are cached by lockfile;
signed release artifacts are transported as artifacts, never dependency caches.
