# Runtime Evidence and Recovery

Reasonix keeps runtime evidence local and outside the provider prompt. Two session-owned stores complement the authoritative transcript:

- `<session>.run-journal.ndjson` is a schema-versioned, append-only Run Journal. It records run boundaries and tool-receipt classifications using counters and SHA-256 digests. Raw prompts, arguments, commands, output, errors, and paths are never persisted. Writes use mode `0600`, `fsync`, monotonic sequences, torn-tail repair, and fail closed on future schema versions.
- `deliveryCheckpoint.evidenceLedger` in `<session>.goal-state.json` is a bounded Goal Evidence Ledger. It retains the latest 64 successful evidence identities, mutation generations, and the generation that passed the live completion gates. It is additive: legacy checkpoint booleans remain authoritative and a restored ledger never bypasses fresh verification, review, or sign-off.

Both stores rotate with the session and are included in current session cleanup. Older goal states without ledger fields load conservatively. Older binaries ignore the new JSON fields; the `.ndjson` journal suffix prevents their `.jsonl` session scanners from treating the sidecar as a conversation.

Every successful mutation advances `mutationGeneration`. Verification, review, inspection, criteria, and sign-off receipts are projected into that generation. Only after the existing host readiness checks pass does `closedGeneration` advance and `pendingMutation` clear. A crash therefore leaves a durable, content-free explanation of the unfinished generation while the next run still has to produce valid live evidence.
