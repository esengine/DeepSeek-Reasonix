export type TurnTreeLayoutMode = "tree" | "lanes" | "metro";

export const TURN_TREE_LAYOUT_MODES: TurnTreeLayoutMode[] = ["tree", "lanes", "metro"];
export const TURN_TREE_LAYOUT_EVENT = "reasonix:turn-tree-layout";

const KEY = "reasonix.turnTree.layout";

export function normalizeTurnTreeLayoutMode(value: unknown): TurnTreeLayoutMode {
  return value === "lanes" || value === "metro" ? value : "tree";
}

export function readTurnTreeLayoutPreference(): TurnTreeLayoutMode {
  if (typeof localStorage === "undefined") return "tree";
  try {
    return normalizeTurnTreeLayoutMode(localStorage.getItem(KEY));
  } catch {
    return "tree";
  }
}

export function saveTurnTreeLayoutPreference(mode: TurnTreeLayoutMode): void {
  if (typeof localStorage !== "undefined") {
    try {
      localStorage.setItem(KEY, mode);
    } catch {
      // Ignore storage failures; the in-memory UI still updates for this session.
    }
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent<TurnTreeLayoutMode>(TURN_TREE_LAYOUT_EVENT, { detail: mode }));
  }
}
