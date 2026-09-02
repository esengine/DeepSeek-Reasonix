// Content-free geometry distilled from the field diagnostics. The original
// exports are intentionally excluded: only anonymous turn windows required to
// reproduce the unloaded-question transaction remain here.
export const unloadedQuestionJumpReplay = {
  targetTurn: 0,
  requestedTurn: 1,
  totalTurns: 994,
  windows: [
    { firstTurn: 561, lastTurn: 994, hasOlderHistory: true, rowCount: 434 },
    { firstTurn: 148, lastTurn: 994, hasOlderHistory: true, rowCount: 847 },
    { firstTurn: 1, lastTurn: 994, hasOlderHistory: false, rowCount: 994 },
  ] as const,
} as const;
