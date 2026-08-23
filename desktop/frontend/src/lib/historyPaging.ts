const HISTORY_PAGE_TURNS = 60;
const HISTORY_JUMP_MAX_TURNS = 500;
const HISTORY_JUMP_MAX_ENTRIES = 1000;

export function historyTurnsToLoad(startTurn: number, totalTurns: number, targetTurn?: number): number {
  const normalizedTarget = Number.isInteger(targetTurn) && (targetTurn ?? 0) > 0
    ? Math.min(Math.max(1, totalTurns), targetTurn as number)
    : undefined;
  const missingTurns = normalizedTarget === undefined
    ? HISTORY_PAGE_TURNS
    : Math.max(HISTORY_PAGE_TURNS, startTurn - normalizedTarget);
  return Math.min(HISTORY_JUMP_MAX_TURNS, missingTurns);
}

export function historyPageRequestBudget(
  startTurn: number,
  totalTurns: number,
  targetTurn?: number,
): { turns: number; entries?: number } {
  const turns = historyTurnsToLoad(startTurn, totalTurns, targetTurn);
  return targetTurn === undefined ? { turns } : { turns, entries: HISTORY_JUMP_MAX_ENTRIES };
}
