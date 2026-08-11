import { GitBranch } from "lucide-react";
import { useT } from "../lib/i18n";

// Adapted from the lightweight worktree identity affordance proposed in #6119.
// One component keeps tabs, the topic bar, and the project tree consistent.
export function WorktreeBadge({ size = 12 }: { size?: number }) {
  const t = useT();
  const label = t("projectTree.isolatedWorktree");
  return (
    <span className="worktree-badge" title={label} aria-label={label}>
      <GitBranch size={size} aria-hidden="true" />
    </span>
  );
}
