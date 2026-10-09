import { t } from "../i18n";

// Removing a folder closes its panes, and closing one stops what it is running.
// That price is said here rather than discovered afterwards — the kernel refuses
// the removal either way, and a refusal names no pane the reader can go find.
export function removeHint(panes: number, live: number): string {
  if (panes === 0) return t("不会删除任何文件");
  if (live === 0) return t("将先关闭 {n} 个面板；不会删除任何文件", { n: panes });
  return t("其中 {live} 个对话正在运行，停止后才能移除", { live });
}

// 确认不跟原来那行抢位置：把「×」换成「移除」两个字，宽度一变就把文件夹名挤扁
// 了。整行换成一条问句，取消永远在手边，误点的代价是零。
export function Confirm({
  what,
  hint,
  go,
  danger,
  onGo,
  onCancel,
}: {
  what: string;
  hint?: string;
  go: string;
  danger?: boolean;
  onGo: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      className="wsconfirm"
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
        <button data-action="layer.dismiss" onClick={onCancel}>{t("取消")}</button>
        <button autoFocus data-action="workspace.remove" data-danger={danger ? "" : undefined} onClick={onGo}>
          {go}
        </button>
      </div>
    </div>
  );
}
