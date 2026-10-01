import { useState } from "react";
import { t } from "../i18n";
import type { TreeWorkspace } from "../port/hub";

const KEY = "reasonix:auto-archive-seen";

function seenAt(): number {
  try {
    return Number(localStorage.getItem(KEY)) || 0;
  } catch {
    return 0;
  }
}

// Says once that the kernel archived conversations on its own, from the rows'
// own marks, so it holds even when the window was closed while it happened.
export function AutoArchiveNotice({ tree }: { tree: TreeWorkspace[] }) {
  const [seen, setSeen] = useState(seenAt);
  const fresh = tree.flatMap((ws) => ws.sessions).filter((s) => s.archived && (s.autoArchivedAt ?? 0) > seen);
  if (fresh.length === 0) return null;
  const dismiss = () => {
    const latest = Math.max(...fresh.map((s) => s.autoArchivedAt ?? 0));
    try {
      localStorage.setItem(KEY, String(latest));
    } catch {
      // the notice just comes back next launch
    }
    setSeen(latest);
  };
  return (
    <div className="errbar" data-kind="note" role="status">
      <span>{t("已自动归档 {n} 个会话，可在“已归档”中恢复。", { n: fresh.length })}</span>
      <button data-action="auto-archive.notice-dismiss" onClick={dismiss}>{t("知道了")}</button>
    </div>
  );
}
