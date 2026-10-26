# Remove tracked prototypes and dead script

Deletes files that are no longer reached by anything in the tree, and moves the
one document still worth reading next to the rest of the design notes. This is
a cleanup, not a bug fix — every deletion is listed below so an owner can check
each one against its reason.

## What is deleted, and why each one

- `output/agent-workbench/`, `output/agent-workbench-v5/`,
  `output/agent-workbench-v6/` — static HTML/CSS/JS design prototypes
  (~1.4 MB tracked). The implementation they prototyped lives in
  `desktop/frontend-next`; nothing in the tree, the build, or the docs links to
  them. `output/` is ignored from this change on (`/output/` added to
  `.gitignore`), so prototypes stop being tracked altogether.
- `prod_fast_test` — a 31-line shell script at the repo root that no Makefile
  target, script, or CI workflow invokes. Grepped for its name across the tree
  before deleting: zero references.
- `design-qa.md` → `docs/design/DESIGN_QA.md` — **moved, not deleted**. The
  design QA notes belong with `docs/design/COMPUTER_USE.md`,
  `docs/design/INTELLIGENT_UI.md` and their neighbours, which is where a reader
  of the design notes already is. Content is unchanged (rename detected as
  `R100`).

## What is not deleted

Nothing under `docs/`, `benchmarks/`, `examples/` or `tools/` is touched. If a
prototype turns out to still be a reference for someone, restoring a directory
from history is a one-line `git checkout` of this commit's parent.

## Reason this waits separately

Deletion of tracked content is an owner's decision, not a side effect of a bug
fix: a reviewer who wants half of it rolled back should not have to also roll
back the fix, and a reviewer of the fix should not have to judge 1.5 MB of
unrelated deletions. Related fix: #14554 (separate PR).
