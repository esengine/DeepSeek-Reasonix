import type { HistoryPaging } from "./useHistoryPages";
import { t } from "../i18n";

export function HistoryPageControl({ paging, load }: { paging?: HistoryPaging; load: () => void }) {
  if (!paging || !paging.hasMore && !paging.busy && !paging.failed) return null;
  return <button type="button" className="sessmore" data-action="history.more" disabled={paging.busy}
    onClick={paging.hasMore ? load : () => { void paging.reload().catch(() => {}); }}>
    {paging.busy ? t("正在读取历史消息…") : paging.failed ? t("历史加载失败 · 重试") : t("加载更早的消息")}
  </button>;
}
