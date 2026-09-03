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

// Anonymous scroll evidence distilled from the Windows field report
// `reasonix-frontend-diagnostics-8a5de879.json`. Text, paths and stable row IDs
// are intentionally omitted; only the geometry needed to prevent regression is
// retained.
export const field9711ScrollReplay = {
  buildCommit: "d9cd713",
  viewport: 555,
  direction: -1,
  result: "restore-anchor",
  transactions: [
    { id: 40, maxReverse: 2_593.02, extentDelta: 5_185 },
    { id: 54, maxReverse: 299.68, extentDelta: 589 },
    { id: 59, maxReverse: 7_252.06, extentDelta: 14_492 },
    { id: 62, maxReverse: 4_586.03, extentDelta: 9_163 },
    { id: 64, maxReverse: 5_685.71, extentDelta: 11_371 },
    { id: 66, maxReverse: 6_836.19, extentDelta: 13_700 },
    { id: 71, maxReverse: 5_342.22, extentDelta: 10_692 },
    { id: 75, maxReverse: 5_217.14, extentDelta: 10_497 },
    { id: 81, maxReverse: 27_104.41, extentDelta: 51_140 },
  ],
} as const;
