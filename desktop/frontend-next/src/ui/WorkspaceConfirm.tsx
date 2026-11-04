import { t } from "../i18n";

// Removing a folder closes its panes, and closing one stops what it is running.
// That price is said here rather than discovered afterwards — the kernel refuses
// the removal either way, and a refusal names no pane the reader can go find.
export function removeHint(panes: number, live: number): string {
  if (panes === 0) return t("session.no_files_are_deleted");
  if (live === 0) return t("session.closes_n_panes_first_no", { n: panes });
  return t("session.live_of_them_are_running", { live });
}

// What deleting a conversation costs, said before it is asked.
export function sessionDeleteHint(copies: number, hasPane: boolean, live: boolean): string {
  if (copies > 0) return t("session.n_recovery_copies_hold_content", { n: copies });
  return t(!hasPane ? "common.deletes_it_together_with_its" : live ? "common.running_stop_it_before_deleting" : "common.its_panel_closes_first");
}

// 确认不跟原来那行抢位置：把「×」换成「移除」两个字，宽度一变就把文件夹名挤扁
// 了。整行换成一条问句，取消永远在手边，误点的代价是零。
export function Confirm({
  what,
  hint,
  go,
  danger,
  alt,
  onGo,
  onCancel,
}: {
  what: string;
  hint?: string;
  go: string;
  danger?: boolean;
  // A wider form of the same removal, offered next to the plain one.
  alt?: { label: string; onGo: () => void };
  onGo: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      className="wsconfirm"
      data-wide={alt ? "" : undefined}
      role="alertdialog"
      aria-label={what}
      data-action-keydown="layer.dismiss"
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return;
        // Dismissing the question is not stopping the run behind it.
        ev.stopPropagation();
        onCancel();
      }}
    >
      <div className="wsconfirm-t">
        <span className="q">{what}</span>
        {hint && <span className="h">{hint}</span>}
      </div>
      <div className="wsconfirm-a">
        <button data-action="layer.dismiss" onClick={onCancel}>{t("common.cancel")}</button>
        {alt && (
          <button data-action="workspace.remove" data-value="with-copies" data-danger={danger ? "" : undefined} onClick={alt.onGo}>
            {alt.label}
          </button>
        )}
        <button autoFocus data-action="workspace.remove" data-danger={danger ? "" : undefined} onClick={onGo}>
          {go}
        </button>
      </div>
    </div>
  );
}
