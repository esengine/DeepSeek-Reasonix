// Why a fold installed nothing, keyed by the kernel's code. A code with no entry
// shows only that nothing folded, never a guess at the cause.
export const FOLD_WHY: Record<string, string> = {
  summary_failed: "compactionWhy.theRequestThatWritesThe",
  summary_timeout: "compactionWhy.theRequestThatWritesThe2",
  summary_ceiling: "compactionWhy.theRequestThatWritesThe3",
  summary_truncated: "compactionWhy.theSummaryHitTheOutput",
  summary_not_digest: "compactionWhy.theSummarizerDidNotReturn",
  summary_input_too_large: "compactionWhy.theContentToFoldStill",
  hook_refused: "compactionWhy.anExtensionRefusedThisFold",
  persist_failed: "compactionWhy.theFoldedResultCouldNot",
  context_changed: "compactionWhy.theConversationChangedWhileThe",
  digest_lost_every_change: "compactionWhy.theSummaryRecordedNoneOf",
  candidate_not_smaller: "compactionWhy.theFoldedContextWasNo",
  candidate_above_ceiling: "compactionWhy.theFoldedContextStillExceeds",
  candidate_above_trigger: "compactionWhy.theFoldedContextWasStill",
  result_above_trigger: "compactionWhy.theFoldedContextIsStill",
  candidate_above_physical_ceiling: "compactionWhy.theFoldedContextStillExceeds2",
  savings_below_minimum: "compactionWhy.theFixedPrefixFillsThe",
  fixed_prefix_above_trigger: "compactionWhy.thePartThatCannotBe",
  fold_empty_after_hooks: "compactionWhy.anExtensionHookEmptiedThe",
  cancelled: "compactionWhy.theCompactionWasCancelled",
  unclassified: "compactionWhy.theCompactionFailedForA",
  busy: "compactionWhy.aTurnOrASession",
  input_unchanged: "compactionWhy.theContextHasNotChanged",
  no_new_closed_prefix: "compactionWhy.nothingHasClosedSinceThe",
  fold_below_economics: "compactionWhy.tooLittleHasBeenAdded",
  active_turn_boundary: "compactionWhy.theTurnInFlightHas",
  no_foldable_region: "compactionWhy.noFoldableRegionRemains",
};

// The reason a /compact notice carries as its detail; an empty detail is the
// kernel's decline with no class.
export const NO_CODE_WHY = "compactionWhy.nothingLeftWorthFolding";
