import "./RestoreNotice.css";
import { t } from "../i18n";
import type { ReloadFailure, RestoreNotice as Receipt } from "./rewind";

export function RestoreNotice({ receipt, onUndo, onDismiss, refreshing }: {
  receipt: Receipt;
  refreshing?: boolean;
  onUndo: (tx: string) => Promise<void>;
  onDismiss: () => void;
}) {
  return <div className="restore-notice" role="status">
    <span>{t("app.n_files_restored", { n: receipt.files })}</span>
    <button data-action="rewind.undo" disabled={receipt.working || refreshing} onClick={() => void onUndo(receipt.tx)}>{t("app.undo_this_restore")}</button>
    <button data-action="layer.dismiss" disabled={receipt.working || refreshing} onClick={onDismiss}>{t("common.got_it")}</button>
    {receipt.error && <span role="alert" tabIndex={0}>{receipt.error}</span>}
  </div>;
}

export function ReloadNotice({ failure, onReload }: { failure: ReloadFailure; onReload: () => Promise<void> }) {
  return <div className="restore-notice" role="status">
    <span>{t("app.the_operation_completed_but_the")}</span>
    <button data-action="session.reload" disabled={failure.working} onClick={() => void onReload()}>{t("app.retry_session_refresh")}</button>
    <span role="alert" tabIndex={0}>{failure.error}</span>
  </div>;
}
