# Generated training corpus

Tasks here exist to produce training data, not to measure anything. The e2e
suite is the instrument: mixing generated tasks into it would change what a
cross-version comparison means, so the two stay apart.

## Bundle contract

    tasks/<id>/
      task.toml    prompt, class, timeout_sec
      verify.sh    the grader; run from the workspace root
      workdir/     the seed the agent starts from
      solution/    the reference fix, overlaid on workdir/

Both halves of the grader are enforced by `cmd/e2ebench/corpus_test.go`:

- `TestSolvableCorpusSeedsMustNotGradeClean` — verify.sh must **fail** on the
  pristine seed, or the task scores the same whether it was solved or ignored.
- `TestCorpusGradersPassTheReferenceSolution` — verify.sh must **pass** with
  solution/ applied, or no attempt can ever satisfy it. `solution/` is optional
  in the hand-authored corpora and **required** here.

A generated task that fails either gate is deleted, never patched into the
baseline.

## Regenerating

    ./generate.sh <count>

Candidates are produced one per specification line in `specs.txt`, validated by
the gates above, and only survivors are kept.
