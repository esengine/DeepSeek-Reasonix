import { forwardRef, useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { normalizeTurnChanges } from "../lib/turnResult";
import type { TurnChanges, TurnFileChange, WireCompletionSummary } from "../lib/types";
import { DiffView } from "./DiffView";
import { TurnResultSummary } from "./TurnResultSummary";
import { WorkspaceTurnVerification } from "./WorkspaceTurnVerification";

function FrozenFileDiff({ tabId, sessionPath, turn, resultId, file }: { tabId: string; sessionPath: string; turn: number; resultId: string; file: TurnFileChange }) {
  const t = useT();
  const key = `${tabId}\u0000${sessionPath}\u0000${resultId}\u0000${file.path}`;
  const [resource, setResource] = useState<{ key: string; file?: TurnFileChange | null }>();
  useEffect(() => {
    let active = true;
    void app.WorkspaceTurnChangeDetail(tabId, sessionPath, turn, resultId, file.path)
      .then(value => { if (active) setResource({ key, file: value }); })
      .catch(() => { if (active) setResource({ key, file: null }); });
    return () => { active = false; };
  }, [file.path, key, resultId, sessionPath, tabId, turn]);
  const detail = resource?.key === key ? resource.file : undefined;
  return <section className="turn-file-diff" aria-label={file.path}>
    <h4>{file.path}</h4>
    {detail === undefined ? <p role="status">{t("completion.loadingResult")}</p>
      : !detail ? <p>{t("completion.diffUnavailable")}</p>
      : detail.binary ? <p>{t("completion.binary")}</p>
      : detail.modeOnly ? <p>{t("completion.modeOnly")}</p>
      : detail.unavailable ? <p>{t("completion.detailLimited")}</p>
      : detail.patch ? <DiffView diff={detail.patch} /> : <p>{t("completion.noNetChanges")}</p>}
  </section>;
}

export const WorkspaceTurnResult = forwardRef<HTMLElement, {
  summary: WireCompletionSummary;
  tabId: string;
  sessionPath: string;
  initialView?: "changes" | "checks";
  qualityFloor?: "standard" | "delivery";
  onAllChanges(): void;
}>(function WorkspaceTurnResult({ summary, tabId, sessionPath, initialView = "checks", qualityFloor, onAllChanges }, ref) {
  const t = useT();
  const recorded = summary.receipt?.diff;
  const resultId = recorded?.id ?? "";
  const turn = recorded?.turn ?? summary.checkpointTurn;
  const key = `${tabId}\u0000${sessionPath}\u0000${resultId}`;
  const [selected, setSelected] = useState<{ key: string; path: string }>();
  const [view, setView] = useState(initialView);
  const [resource, setResource] = useState<{ key: string; result?: TurnChanges }>();
  useEffect(() => {
    if (!resultId || turn === undefined || !sessionPath) return;
    let active = true;
    void app.WorkspaceTurnChanges(tabId, sessionPath, turn, resultId)
      .then(result => { if (active) setResource({ key, result: normalizeTurnChanges(result) }); })
      .catch(() => { if (active) setResource({ key }); });
    return () => { active = false; };
  }, [key, resultId, sessionPath, tabId, turn]);
  const current = resource?.key === key ? resource : undefined;
  const available = current?.result?.id === resultId && current?.result?.coverage !== "unknown";
  const files = recorded?.files ?? [];
  const file = selected?.key === key ? files.find(file => file.path === selected.path) : undefined;
  return <section ref={ref} className="workspace-turn-result" aria-label={t("notice.completionChangesTitle")}>
    <header className="workspace-turn-result__head">
      <div className="workspace-turn-result__tabs" role="group" aria-label={t("notice.completionChangesTitle")}>
        <button className="btn btn--small" aria-pressed={view === "changes"} onClick={() => setView("changes")}>{t("completion.turnDiff")}</button>
        <button className="btn btn--small" aria-pressed={view === "checks"} onClick={() => setView("checks")}>{t("completion.panelTitle")}</button>
      </div>
      <button className="btn btn--small" onClick={onAllChanges}>{t("completion.allChanges")}</button>
    </header>
    {turn !== undefined && <p className="workspace-turn-result__label">{t("completion.turnLabel", { count: turn + 1 })}</p>}
    {view === "checks" ? <WorkspaceTurnVerification summary={summary} qualityFloor={qualityFloor} tabId={tabId} sessionPath={sessionPath} /> : <>
      <TurnResultSummary summary={summary} />
      {(!resultId || !sessionPath || current && !available) && <p className="workspace-note">{t("completion.diffUnavailable")}</p>}
      {resultId && sessionPath && !current && <p role="status">{t("completion.loadingResult")}</p>}
      <div className="turn-file-list">
        {files.map(entry => <button className="turn-file-list__entry" key={entry.path} disabled={!available} aria-pressed={file?.path === entry.path} onClick={() => setSelected({ key, path: entry.path })}>
          <span>{entry.path}</span><span>{entry.binary ? t("completion.binary") : entry.modeOnly ? t("completion.modeOnly") : entry.uncounted ? t("completion.linesUnknown") : <><span className="turn-result-added">+{entry.added}</span>{" "}<span className="turn-result-removed">−{entry.removed}</span></>}</span>
        </button>)}
      </div>
      {file && available && turn !== undefined && <FrozenFileDiff tabId={tabId} sessionPath={sessionPath} turn={turn} resultId={resultId} file={file} />}
    </>}
  </section>;
});
