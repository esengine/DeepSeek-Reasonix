// dockEntries lists the right-dock entry points shown in the floating
// launcher (and used by App to map an entry id to its default tab): an id, a
// translated label key, and the default tab type each entry opens. Kept
// icon-free so App can import it synchronously without pulling lucide into
// the initial bundle — DockLauncher (lazy) attaches the icons.
//
// Scope note: only the three existing views (files / changed / context) are
// exposed for now — the dock is the base extension surface and other entries
// (terminal, browser, remote, instructions) are intentionally not listed yet.

import type { TabType } from "../../store/activityBar";

export interface DockEntryConfig {
  id: string;
  labelKey: string;
  defaultTab: TabType;
}

export const ACTIVITY_BAR_ENTRIES: DockEntryConfig[] = [
  { id: "context", labelKey: "rightDock.overview", defaultTab: "context" },
  { id: "files", labelKey: "workspace.filesTab", defaultTab: "file" },
  { id: "changed", labelKey: "workspace.changedTab", defaultTab: "changed" },
];
