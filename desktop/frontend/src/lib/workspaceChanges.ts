import type { Translator } from "./i18n";
import type { DictKey } from "../locales/en";

// git-status(1) "Unmerged" table: DD/AU/UD/UA/DU/AA/UU. AA and DD carry no U,
// so they must be matched as whole codes before the per-letter fallbacks.
const UNMERGED = new Set(["DD", "AU", "UD", "UA", "DU", "AA", "UU"]);

export function workspaceGitStatusLabelKey(status?: string): DictKey | null {
  const s = (status ?? "").trim();
  if (!s) return null;
  if (s === "??") return "workspace.gitStatusUntracked";
  if (UNMERGED.has(s)) return "workspace.gitStatusUnmerged";
  if (s.includes("R")) return "workspace.gitStatusRenamed";
  if (s.includes("C")) return "workspace.gitStatusCopied";
  if (s.includes("D")) return "workspace.gitStatusDeleted";
  if (s.includes("A")) return "workspace.gitStatusAdded";
  if (s.includes("M")) return "workspace.gitStatusModified";
  return null;
}

/** Porcelain status code as a reader-facing label; unknown codes pass through. */
export function workspaceGitStatusLabel(status: string | undefined, t: Translator): string {
  const key = workspaceGitStatusLabelKey(status);
  return key ? t(key) : (status ?? "").trim();
}
