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
    <span>{t("已还原 {n} 个文件", { n: receipt.files })}</span>
    <button data-action="rewind.undo" disabled={receipt.working || refreshing} onClick={() => void onUndo(receipt.tx)}>{t("撤销这次还原")}</button>
    <button data-action="layer.dismiss" disabled={receipt.working || refreshing} onClick={onDismiss}>{t("知道了")}</button>
    {receipt.error && <span role="alert" tabIndex={0}>{receipt.error}</span>}
  </div>;
}

export function ReloadNotice({ failure, onReload }: { failure: ReloadFailure; onReload: () => Promise<void> }) {
  return <div className="restore-notice" role="status">
    <span>{t("操作已完成，但会话刷新失败")}</span>
    <button data-action="session.reload" disabled={failure.working} onClick={() => void onReload()}>{t("重试刷新会话")}</button>
    <span role="alert" tabIndex={0}>{failure.error}</span>
  </div>;
}
