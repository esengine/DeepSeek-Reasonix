export type LayoutSizeKey =
  | "sidebarWidth"
  | "sidebarWidthGraphite"
  | "rightDockWidth"
  | "rightDockTreeWidth"
  | "rightDockPreviewWidth"
  | "workspaceFileTreePanelWidth"
  | "workspaceTreeWidth"
  | "composerHeight"
  | "drawerWidth"
  | "settingsDrawerWidth";

type LayoutPreferences = {
  sizes?: Partial<Record<LayoutSizeKey, number>>;
};

const STORAGE_KEY = "reasonix.layoutPreferences.v1";

const LEGACY_SIZE_KEYS: Record<LayoutSizeKey, string[]> = {
  sidebarWidth: ["reasonix.sidebar.width"],
  sidebarWidthGraphite: [],
  rightDockWidth: [],
  rightDockTreeWidth: [],
  rightDockPreviewWidth: [],
  workspaceFileTreePanelWidth: [],
  workspaceTreeWidth: ["reasonix.workspaceTree.width"],
  composerHeight: ["reasonix.composerHeight"],
  drawerWidth: ["reasonix.drawer.width"],
  settingsDrawerWidth: ["reasonix.settingsDrawer.width"],
};

type ClampSize = (value: number) => number;

function readPrefs(): LayoutPreferences {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as LayoutPreferences;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function writePrefs(prefs: LayoutPreferences): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ sizes: prefs.sizes ?? {} }));
  } catch {
    /* ignore storage failures */
  }
}

function readLegacySize(key: LayoutSizeKey): number | null {
  if (typeof window === "undefined") return null;
  for (const legacyKey of LEGACY_SIZE_KEYS[key]) {
    try {
      const raw = Number(window.localStorage.getItem(legacyKey));
      if (Number.isFinite(raw) && raw > 0) return raw;
    } catch {
      /* keep trying other keys */
    }
  }
  return null;
}

function normalizeSize(value: number, clamp?: ClampSize): number {
  const rounded = Math.round(value);
  return clamp ? clamp(rounded) : rounded;
}

export function loadLayoutSize(key: LayoutSizeKey, fallback: number, clamp?: ClampSize): number {
  const prefs = readPrefs();
  const saved = prefs.sizes?.[key];
  const value = Number.isFinite(saved) && saved! > 0 ? saved! : readLegacySize(key);
  return value === null ? normalizeSize(fallback, clamp) : normalizeSize(value, clamp);
}

export function loadOptionalLayoutSize(key: LayoutSizeKey, clamp?: ClampSize): number | null {
  const prefs = readPrefs();
  const saved = prefs.sizes?.[key];
  const value = Number.isFinite(saved) && saved! > 0 ? saved! : readLegacySize(key);
  return value === null ? null : normalizeSize(value, clamp);
}

export function saveLayoutSize(key: LayoutSizeKey, value: number, clamp?: ClampSize): void {
  const prefs = readPrefs();
  const sizes = { ...(prefs.sizes ?? {}), [key]: normalizeSize(value, clamp) };
  writePrefs({ ...prefs, sizes });
}

export function clearLayoutSize(key: LayoutSizeKey): void {
  const prefs = readPrefs();
  const sizes = { ...(prefs.sizes ?? {}) };
  delete sizes[key];
  writePrefs({ ...prefs, sizes });
}
