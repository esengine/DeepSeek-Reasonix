# Context retrieval bench

Two questions about long-horizon memory, and the harness that makes their
answers trustworthy.

    -mode=preflight     build all 18 tasks under every arm and check the contract
    -mode=snippet       does a rank-1 hit hand over the whole answer?
    -mode=adversarial   send a model after the answer through every host surface
    -mode=run-search    Experiment R: is searchable canonical a real capability?
    -mode=run-boundary  Experiment I, paired: the 6 efficiency tasks across their boundary
    -mode=run-index     Experiment I, full: the 6 efficiency tasks x 4 budgets
    -mode=calibrate     find each index task's PlantAfterGen for its cue tier

`-dry` drives any run mode with a scripted provider, for nothing. Real runs need
`DEEPSEEK_API_KEY`.

## Status

**Searchable canonical recall: proven.** Six of six recovered with search on,
three of six with it off, and the successes without it came from enumerating
addresses by hand at roughly nine times the page-in cost (139 vs 1292
tokens/task). Measured after three separate leaks were closed, so the numbers
are from a clean environment.

**Fold index marginal utility: unresolved.** Both the positive and the negative
estimates are invalidated. The first Stage 2 ran in a contaminated environment.
The clean paired batch found a consistent effect (six of six tasks searched less
and paged in less with the cue visible), and the same-batch dose-response did
not reproduce it (`boundary-aligned 0/4`, recall-token delta reversing sign).
The most likely reason is that two of the six index tasks carry heavy-tail
stopping behaviour that swamps the effect being measured.

Existing evidence is not sufficient to tune the shipped 1% budget in either
direction. Reopening this wants a new corpus, not more samples: that substrate
now exists, two qualifying tasks per cue tier — see "Index corpus v2" below.

**Things that turned out not to be problems**, each after being measured rather
than argued about:

| Suspected | Measured |
| --- | --- |
| Model writes poor queries (2.17 searches/task) | 22/23 found the target on query 1; the extra searches are one round's parallel fan-out |
| Model reaches for the workspace before memory (7/11) | Linear event order misread; by model round it is MemoryFirst 4/6 |
| Snippets too short for multi-value answers | 240 runes covers 12/12 tasks at 100%; records are 30-179 runes |
| A general stopping failure | 37/44 runs issued no search after the answer was in hand; all 7 that did belong to 2 tasks |

## Measurement distinctions this bench had to learn

Each of these reversed a conclusion once. They are enforced in the schema now,
not left to whoever reads the numbers next.

- **TargetHit is not EvidenceSufficient.** A rank-1 hit can return a window too
  short to answer with. `FirstHitCoverage` records how much of the scored answer
  the first hit actually handed over.
- **Linear event order is not model-round order.** Two calls in one round are a
  parallel fan-out, not a preference. Routing compares rounds.
- **An isolated workdir is not an isolated host.** The sandbox mounts the host
  read-only by design. Three leaks were found and closed: the corpus source, the
  fixture transcript, and its event log sidecar.
- **A count is not its evidence.** Query text, snippet contents and trajectories
  are all persisted now, because three analyses in a row needed a paid re-run to
  ask a question of data already collected.

## How a run stays honest

The corpus holds templates, never answers: every scored literal is a `{{var}}`
instantiated per run, so grepping this directory reveals the question and not
the answer. `TestNoAnswerLiteralExistsInTheRepository` asserts it with
`git grep`. Preflight rebuilds every arm and checks the answer is unreadable in
the real `provider.Request`, the probe query ranks the target within five, and
an index task's cue sits at exactly the scales its tier names. The fixture is
removed from disk once the agent holds it in memory, and the directory is
scanned for answer literals before the first provider request and again after
the turn. Any answer reaching the model through a tool that is not `recall`
marks the run contaminated and takes it out of every statistic.

## Index corpus v2

The efficiency substrate is six tasks that hold on both arms: `i37-dispatchlag`
and `i118-fencelag` (quarter), `i106-budgetseal` and `i70-ingestfloor` (half),
`i47-tenantlease` and `i112-lagquota` (default).

They came out of five batches of candidates, each screened the same way: one
`-mode=run-index` pass over the batch, then two more only for the candidates that
were clean on both boundary arms, and only the candidates that held on both arms
in all three passes stayed. A candidate that does not hold is dropped, not
promoted. The numbers are candidates, per tier:

| batch | quarter | half | default |
| --- | --- | --- | --- |
| first pool, `i31`-`i54` | 1 of 8 | 0 of 8 | 2 of 8 |
| second pool, `i61`-`i84` | 1 of 4 screened | 4 of 8 | 0 of 8 |
| third pool, `i85`-`i108` | 2 of 8 | 8 of 16 | none authored |
| replacement pool, `i109`-`i116` | - | - | 4 of 8 |
| replacement pool, `i92`, `i117`-`i124` | 3 of 9 | - | - |

That table counts candidates that held on both arms in all three passes, which is
not the size of the substrate. 89 candidates were authored, 77 were screened (the
second pool's quarter tier stopped at 4 of 8 and its default tier was never run),
and 25 held: 7 quarter, 12 half, 6 default. The substrate keeps two per tier, so
the other 19 survivors are spares rather than corpus members. Candidates from the
earlier round are not counted here.

| task | tier | cue present | cue absent |
| --- | --- | ---: | ---: |
| `i37-dispatchlag` | quarter | 3/3 | 3/3 |
| `i118-fencelag` | quarter | 3/3 | 3/3 |
| `i106-budgetseal` | half | 3/3 | 3/3 |
| `i70-ingestfloor` | half | 3/3 | 3/3 |
| `i47-tenantlease` | default | 3/3 | 3/3 |
| `i112-lagquota` | default | 3/3 | 3/3 |

Across their 36 judged cells: `SnippetStop` 23, `SnippetThenRead` 10 and
`DefensiveRead` 3 - every cell clean, nothing post-sufficient and nothing
`NeverSufficient`.

Four of the first six were flagged in review for sharing answer values, probe
queries and prompt wording within a tier. Separating them and re-screening those
four changed two of them: `i37` and `i47` held, `i62` and `i51` did not, so they
were replaced - `i90-recoverylag` was tried first and did not hold either, and
`i118-fencelag` and `i112-lagquota` are what did. The separation was not
cosmetic: the shared wording had been carrying two of the six.

Nothing in the task shape separates the survivors from the rest: within the half
tier, plain-number answers survived 0 of 8 in the first pool and 4 of 8 in the
second, and codename answers survived 8 of 16 in the third. An earlier claim here
that the answer kind decides whether a candidate holds does not survive this
table and is withdrawn. The dirty class was `PostSufficientRetrieval` in most
failures - the model re-checking a value it already had - but which tasks hit it
was not predictable from their shape at this sample.

`i01`-`i06` are the frozen study's six tasks, kept as `roleStopping`. Measured
once each with `-mode=run-index -task <id>`, per task and per arm:

| task | tier | cue present | cue absent |
| --- | --- | --- | --- |
| `i01-transport-fallback` | quarter | SnippetStop | DefensiveRead |
| `i02-scheduler-handoff` | quarter | PostSufficientRetrieval | PostSufficientRetrieval |
| `i03-coalescing` | half | PostSufficientRetrieval | PostSufficientRunaway |
| `i04-recovery-fence` | half | DefensiveRead | PostSufficientRunaway |
| `i05-cache-probe` | default | SnippetThenRead | PostSufficientRetrieval |
| `i06-dispatch-handoff` | default | SnippetStop | PostSufficientRetrieval |

Five of the six show post-sufficient behaviour in at least one arm (`i02`-`i06`),
and `i01` is clean on both. The cue-present arm is clean for four of them
(`i01`, `i04`, `i05`, `i06`), which is why they stay in the corpus as the
stopping side rather than being retitled. All six are excluded from `run-index`
and `run-boundary` batches and stay measurable one at a time with `-task <id>`. A
task added to the corpus declares a role, so it cannot arrive without one.

A task qualifies only if it is clean on both of its boundary arms - the
cue-present run and the cue-absent run - in every run of a screening batch. Clean
means a `stopping_class` other than `PostSufficientRetrieval` or
`PostSufficientRunaway`: `SnippetStop`, `SnippetThenRead` and `DefensiveRead`
all qualify, and a defensive read is deliberately not counted as waste. The
class comes from the three counts in that report — `searches_after_sufficient`,
`reads_after_sufficient` and `escapes_after_sufficient` — together with how
sufficiency was reached and how many rounds followed it. The remaining
requirements still apply — successful tool call, answer in the output rather
than the cue, rank 1, `FirstHitCoverage` complete, one or two simple values,
nothing in the workspace.

`-mode=calibrate` is not a screening tool: it builds and reads back the
fixtures, and reads nothing else, so its cue-visibility profile is the same for
every task.

Then repeats rather than levels: cue-present against cue-absent on each task's
own boundary, three runs per cell. What is unknown is the variance, and four
budgets sampled once each cannot show it.
