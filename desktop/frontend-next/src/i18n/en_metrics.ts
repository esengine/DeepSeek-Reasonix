// The metrics rail: what a turn is costing, what filled the window, and what it
// touched outside the transcript. Split out for the same reason en_settings and
// en_kernel were — one screen's worth of wording, read together.
export const EN_METRICS: Record<string, string> = {
  "metrics.largest": "Largest",
  "contextSummaryCard.runningTurn": "Running turn",
  "contextSummaryCard.showTheLargestItemsIn": "Show the largest items in {name}",
  "contextSummaryCard.largestItemsIn": "Largest items in {name}",
  "metrics.running": "{n} running",
  "cache.prefixCache": "Prefix cache",
  "files.worktreeChanges": "Worktree changes",
  "metrics.noBackgroundJobsRunning": "No background jobs running",
  "files.autoApproved": "auto-approved",
  "files.forReview": "for review",
  "cache.prefixChanged": "prefix changed",
  "metrics.prefixHeld": "prefix held",
  "cache.bodyChanged": "body changed",
  "metrics.bodyHeld": "body held",
  "metrics.messagesCarriedOver": "Messages carried over",
  "cache.prefixHash": "Prefix hash",
  "cost.fallbackPrice": "fallback price",
  "cost.settled": "settled",
  "cost.thisTurn": "this turn ",
  "cost.asQuoted": "as quoted",
  "cost.twoSettlementCurrenciesDoNot": "Two settlement currencies do not add up — one total would mean inventing a rate.",
  "cache.toolSchema": "Tool schema",
  "metrics.remoteHosts": "Remote hosts",
  "metrics.noRequestsYet": "no requests yet",
  "context.nobodyHasSaidHowLarge":
    "Nobody has said how large this source's window is, so there is no share to draw — and no automatic compaction either. A relay forwards somebody else's model, so only you know what it holds.",
  "context.whoeverEnteredThisWindowMay":
    "Whoever entered this window may not have meant this model — click to set what it actually holds",
  "context.appliesToThisModelAlone":
    "Applies to this model alone; the others on this source are left as they are. The context ceiling from the model's docs, not its max output.",
  "context.theContextCeilingFromThe":
    "The context ceiling from the model's docs, not its max output.",
  "rowCount.sessions2": "{n} sessions",
  "cost.tokensPerRound": "Tokens per round",
  "cost.peakAvg": "peak {peak} · avg {avg}",
  "changePreview.readingThisFileSChanges": "Reading this file's changes…",
  "changePreview.thisFileNowMatchesThe": "This file now matches the last commit",
  "common.tooLongToShowIn": "Too long to show in full — this is the start of it",
  "files.editedTimes": "edited {n} times",
  "files.modified": "Modified",
};
