import type { HistoryPaging } from "./useHistoryPages";
import { t } from "../i18n";

export function HistoryPageControl({ paging, load }: { paging?: HistoryPaging; load: () => void }) {
  if (!paging || !paging.hasMore && !paging.busy && !paging.failed) return null;
  return <button type="button" className="sessmore" data-action="history.more" disabled={paging.busy}
    onClick={paging.hasMore ? load : () => { void paging.reload().catch(() => {}); }}>
    {paging.busy ? t("historyPageControl.readingHistoryMessages") : paging.failed ? t("historyPageControl.historyCouldNotBeLoaded") : t("historyPageControl.loadEarlierMessages")}
  </button>;
}
