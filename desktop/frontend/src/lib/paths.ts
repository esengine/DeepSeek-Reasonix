/**
 * Normalize a filesystem path by resolving .worktrees/<name> to its parent
 * project root. Mirrors the Go-side config.ProjectRootFromWorktree.
 *
 * Examples:
 *   /project/.worktrees/feat-x/cmd  → /project
 *   /project/.worktrees/feat-x       → /project
 *   /project/.worktrees              → /project
 *   /project/src                     → /project/src   (unchanged)
 */
export function projectIdentityRoot(path?: string): string {
  const raw = (path ?? "").trim();
  if (!raw) return "";
  const parts = raw.split(/[\\/]/);
  const idx = parts.indexOf(".worktrees");
  if (idx <= 0) return raw;
  return parts.slice(0, idx).join(raw.includes("\\") ? "\\" : "/") || raw;
}
