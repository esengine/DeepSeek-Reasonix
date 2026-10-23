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

## Command completion

The menu includes the command already typed in full as its first, selected row,
followed by matching alternatives. For example, `/reload` offers `/reload`
(reload the runtime) before `/reload-cmd` (reload custom commands).

The kernel sends the exact match separately as `typed`; Studio restores it to
the menu, as the terminal frontend does. Confirming that row closes the menu
without changing the draft; the next Enter sends it.

## Large-file shell regression

From the repository root, install the frontend and Electron dependencies, then
build the host and production page before running the real-shell regression:

```sh
pnpm --dir desktop/frontend-next install --frozen-lockfile
pnpm --dir desktop/electron install --frozen-lockfile
go build -o desktop/electron/bin/ ./cmd/reasonix-studio-host
pnpm --dir desktop/frontend-next build
node desktop/electron/test/attachments_live.mjs
```

The test uses a disposable workspace, Reasonix home and Electron profile. It
checks 111 MiB native-file selection, paste and drop without byte uploads,
then streams a 65 MiB virtual file and verifies its saved size and ready chip.
No provider request is sent.

Set `REASONIX_TEST_ELECTRON` to use an existing Electron executable, or
`REASONIX_ATTACHMENT_ARTIFACTS` to retain a screenshot and JSON result in a
chosen directory. Failed runs retain their fixture directory for diagnosis.

Attachment transport limits are documented in the
[English guide](../../docs/GUIDE.md#studio-file-attachments) and
[Chinese guide](../../docs/GUIDE.zh-CN.md#studio-文件附件).
