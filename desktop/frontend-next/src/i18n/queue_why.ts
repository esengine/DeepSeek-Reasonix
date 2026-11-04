// Why a held queue entry stopped, keyed by the kernel's block code. A code with
// no entry shows the kernel's own diagnostic.
export const BLOCK_WHY: Record<string, string> = {
  steer_unapplied: "kernel.this_guidance_was_not_delivered",
  turn_snapshot_failed: "kernel.the_turn_ended_but_the_2",
  turn_ack_failed: "kernel.the_turn_ended_but_the",
  owner_inactive: "kernel.the_session_that_was_running",
  manifest_salvaged: "kernel.the_queue_record_was_corrupt",
  foreign_images: "kernel.this_entry_was_queued_with",
};
