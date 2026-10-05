---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-22
---

# Reasonix Studio frontend

## Tests

`pnpm test` runs one worker per test file on the threads pool. Threads rather
than forks: the same worker-per-file isolation, cheaper to start, which is most
of what a suite of many small files pays. About 19s to about 15s.

Sharing one worker across files (`isolate: false`) was tried and taken back
out. It reached about 9s and made the suite intermittent:

- `vi.mock` cannot see a module another file already imported unmocked, so the
  mock silently does nothing and the test passes alone and fails in the suite.
- A render test then failed in one full run and passed in the next, with no
  change in between.

A gate that goes red by arrangement teaches people to re-run it rather than
read it, which costs more than the seconds it saves.

`css: true` is load-bearing: the guards under `src/styles` read `app.css`
through `?raw`, and a stubbed stylesheet hands them an empty string, which
passes on nothing.
