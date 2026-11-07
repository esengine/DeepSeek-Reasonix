// Why a fold installed nothing, keyed by the kernel's code. A code with no entry
// shows only that nothing folded, never a guess at the cause.
export const FOLD_WHY: Record<string, string> = {
  summary_failed: "kernel.the_request_that_writes_the_3",
  summary_timeout: "kernel.the_request_that_writes_the_2",
  summary_ceiling: "kernel.the_request_that_writes_the",
  summary_truncated: "kernel.the_summary_hit_the_output",
  summary_not_digest: "kernel.the_summarizer_did_not_return",
  hook_refused: "kernel.an_extension_refused_this_fold",
  persist_failed: "kernel.the_folded_result_could_not",
  context_changed: "kernel.the_conversation_changed_while_the",
  candidate_not_smaller: "kernel.the_folded_context_was_no",
  candidate_above_ceiling: "kernel.the_folded_context_still_exceeds_2",
  candidate_above_trigger: "kernel.the_folded_context_was_still",
  result_above_trigger: "kernel.the_folded_context_is_still",
  candidate_above_physical_ceiling: "kernel.the_folded_context_still_exceeds",
  savings_below_minimum: "kernel.the_fixed_prefix_fills_the",
  fixed_prefix_above_trigger: "kernel.the_part_that_cannot_be",
  fold_empty_after_hooks: "kernel.an_extension_hook_emptied_the",
  cancelled: "kernel.the_compaction_was_cancelled",
  unclassified: "kernel.the_compaction_failed_for_a",
  busy: "kernel.a_turn_or_a_session",
  input_unchanged: "kernel.the_context_has_not_changed",
  no_new_closed_prefix: "kernel.nothing_has_closed_since_the",
  fold_below_economics: "kernel.too_little_has_been_added",
  active_turn_boundary: "kernel.the_turn_in_flight_has",
  no_foldable_region: "kernel.no_foldable_region_remains",
};

// The reason a /compact notice carries as its detail; an empty detail is the
// kernel's decline with no class.
export const NO_CODE_WHY = "kernel.nothing_left_worth_folding";
