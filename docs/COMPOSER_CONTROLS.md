# Desktop composer controls

[简体中文](COMPOSER_CONTROLS.zh-CN.md)

Use the **+** menu to attach content or enable Plan, Goal, or Delivery mode.
Normal execution and Standard delivery are the defaults. Active modes appear
as removable chips; removing Delivery restores Standard without changing Plan
or Goal. Approval policy remains a separate Ask/Auto/Yolo menu. Model and
reasoning effort have independent selectors; unsupported models hide effort.
The status bar no longer repeats the model name. Its turn cost uses two decimal
places; detailed cost values retain their existing precision.

The context ring opens usage details. **Turn time** excludes waits on the user,
whether an approval, an answer, or an MCP interaction, and stops at the
controller's completion timestamp. Retry time remains part of the turn. Turn
tokens and throughput remain available during waits, retries, and after
completion. In-flight tokens are estimated from character density rather than a
flat four characters per token: ASCII prices at roughly four characters per
token and CJK at roughly 1.3. The ring's throughput and token rows are
turn-scoped, counting the turn's cumulative output and carrying the estimate cue
while streaming; the status bar's throughput item covers the most recent request
and its turn-token item is the turn's prompt-plus-output total. **Session time**
is the separately reported session aggregate. Starting a new turn resets turn
metrics. These live metrics are not a persisted historical report. Completed
metrics are settled once per turn; later background-job updates do not replace
them.

The composer defaults to 140px and preserves manual resizing. Running work
uses a theme-aware perimeter trace; reduced motion uses a static outline. Above
the input, a run strip names the current state and, while a turn is live, pins
its readings to the right: the turn clock, the running token total, and
throughput. Colour and position do the separating, not punctuation. The clock
reads first, so the strip answers "is this stuck?"; only the token reading
carries the estimate cue, since the clock is exact and throughput derives from
the reading. A narrow strip spends the state word's width first, never cuts a
reading mid-number, and drops the rate whole below its threshold. Throughput
also appears only while the model is emitting, so a rate frozen by a wait is
never shown as a current speed. The strip's live region still announces the
stable state text alone. Approval, answer, and retry notices remain visible.

The bottom status bar combines workspace and branch into one item: it shows
the branch name, with both workspace path and branch in the tooltip. Non-Git
workspaces show the workspace name. Older item lists are deduplicated. For a
model-only legacy configuration, migration retains only workspace and branch. On the
first upgrade, status labels default to icons; subsequent manual text/icon
choices are preserved. An older binary that rewrites preferences can remove
the upgrade marker, causing the icon default to apply again on re-upgrade.
