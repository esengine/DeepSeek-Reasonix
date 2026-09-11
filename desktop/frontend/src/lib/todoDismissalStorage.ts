const DISMISSED_TODO_STORAGE_KEY = "todoPanel:dismissedKeys";
const MAX_DISMISSED_TODO_KEYS = 160;

export function loadDismissedTodoKeys(): Set<string> {
  try {
    const saved = window.localStorage.getItem(DISMISSED_TODO_STORAGE_KEY);
    if (!saved) return new Set();
    const parsed = JSON.parse(saved) as unknown;
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((value): value is string => typeof value === "string" && value.length > 0));
  } catch {
    return new Set();
  }
}

export function saveDismissedTodoKeys(keys: ReadonlySet<string>): void {
  try {
    window.localStorage.setItem(
      DISMISSED_TODO_STORAGE_KEY,
      JSON.stringify(Array.from(keys).slice(-MAX_DISMISSED_TODO_KEYS)),
    );
  } catch {
    /* ignore quota errors */
  }
}
