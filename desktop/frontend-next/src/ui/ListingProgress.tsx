import type { TreeWorkspace } from "../port/hub";
import { t } from "../i18n";

export function ListingProgress({ workspace, searching, onRetry }: { workspace: TreeWorkspace; searching: boolean; onRetry?: () => void }) {
  if (workspace.countsComplete !== false) return null;
  const failed = workspace.listingState === "degraded";
  return <small className="sessnote" role="status" aria-busy={!failed}>
    {failed ? t("workbench.some_history_summaries_are_unavailable") : searching ? t("workbench.still_searching_some_historical_conversations") : t("workbench.reading_history_summaries")}
    {failed && onRetry && <>{" "}<button type="button" className="ws-reading-retry" data-action="tree.retry" onClick={onRetry}>{t("common.retry")}</button></>}
  </small>;
}
