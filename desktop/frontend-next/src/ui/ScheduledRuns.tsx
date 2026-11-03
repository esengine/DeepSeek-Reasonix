import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, ScheduleBoard, ScheduleDraft, SchedulePreview, ScheduleResultView, ScheduleRunView, ScheduleView } from "../port/port";
import { Group } from "./Group";

const EVERY: [number, string][] = [[3600, "scheduledRuns.everyHour"], [7200, "scheduledRuns.every2Hours"], [21600, "scheduledRuns.every6Hours"], [43200, "scheduledRuns.every12Hours"], [86400, "common.everyDay"], [604800, "scheduledRuns.everyWeek"]];
const POLL_MS = 30_000;

const when = (unix: number) => new Date(unix * 1000).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
const base = (path: string) => path.split(/[\\/]/).filter(Boolean).pop() ?? path;

function trigger(s: ScheduleView): string {
  if (s.atUnix) return t("scheduledRuns.once", { at: when(s.atUnix) });
  const hit = EVERY.find(([sec]) => sec === s.everySec);
  return hit ? t(hit[1]) : t("scheduledRuns.everyMinutes", { n: Math.round((s.everySec ?? 0) / 60) });
}

const STATUS: Record<string, string> = { active: "common.running", paused: "common.held", expired: "scheduledRuns.expired", "circuit-open": "scheduledRuns.stoppedByTheSafetyBreaker" };
const PAUSE: Record<string, string> = { user: "scheduledRuns.youPausedIt", consecutive_failures: "scheduledRuns.pausedAutomaticallyAfterRepeatedFail", breaker: "scheduledRuns.pausedAutomaticallyToProtectThe", lifetime_budget: "scheduledRuns.theLifetimeBudgetIsUsed", corrupt: "scheduledRuns.itsRecordIsDamaged" };
const RUN: Record<string, string> = {
  succeeded: "common.done", failed: "common.failed", blocked: "scheduledRuns.waitingForYou", budget_stopped: "scheduledRuns.budgetUsedUp", interrupted: "scheduledRuns.interrupted", skipped: "scheduledRuns.skipped", claimed: "scheduledRuns.aboutToStart", running: "common.running",
};
const SKIP: Record<string, string> = { missed: "scheduledRuns.missedItsTime", budget_window: "scheduledRuns.theBudgetWindowIsFull", lease_held: "scheduledRuns.anotherRunWasInProgress", concurrency: "scheduledRuns.anotherRunWasInProgress" };

/** Read-only tasks a person confirmed, run at a time they chose. A creation is
 *  shown back as a preview with the most it can spend, and only that preview's
 *  own confirmation creates it. A report is the model's text: it is labelled as
 *  such and drawn as plain text, never as markup. */
export function ScheduledRuns({ port }: { port: AgentPort }) {
  const [board, setBoard] = useState<ScheduleBoard | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [form, setForm] = useState<ScheduleDraft | null>(null);
  const [preview, setPreview] = useState<SchedulePreview | null>(null);
  const [confirmDelete, setConfirmDelete] = useState("");
  const [result, setResult] = useState<ScheduleResultView | null>(null);

  const [loadError, setLoadError] = useState("");

  // A failed read keeps the last board and says why: the block vanishing would
  // leave a person who just created a schedule unable to tell whether it exists.
  const refresh = useCallback(
    () => port.schedules().then((b) => { setBoard(b); setLoadError(""); }).catch((e) => setLoadError(reason(e))),
    [port],
  );
  useEffect(() => {
    let alive = true;
    const load = () => { if (alive) void refresh(); };
    load();
    const id = setInterval(load, POLL_MS);
    return () => { alive = false; clearInterval(id); };
  }, [refresh]);

  const run = useCallback(async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
      await refresh();
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === "schedule.editing_disabled") setReadOnly(true);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  }, [refresh]);

  const unreadable = loadError && (
    <div className="peerform" role="alert">
      <div className="why">{board ? t("scheduledRuns.couldNotRefreshTheScheduled", { reason: loadError }) : t("scheduledRuns.couldNotReadTheScheduled", { reason: loadError })}</div>
      <span className="acts"><button className="act" data-action="schedule.retry" onClick={() => void refresh()}>{t("common.retry")}</button></span>
    </div>
  );
  if (!board) {
    return unreadable ? (
      <Group id="scheduled-runs" title={t("common.scheduledRuns")} hint={t("scheduledRuns.runsTasksReadOnlyAt")}>
        <div className="box">{unreadable}</div>
      </Group>
    ) : null;
  }
  const locked = busy || readOnly;
  const name = (s: ScheduleView) => s.label || base(s.workspace);
  const scheduleName = (r: ScheduleRunView) => { const s = board.schedules.find((x) => x.id === r.scheduleId); return s ? name(s) : r.scheduleId; };
  const draft = (patch: Partial<ScheduleDraft>) => setForm((f) => (f ? { ...f, ...patch } : f));

  return (
    <Group id="scheduled-runs" title={t("common.scheduledRuns")} hint={t("scheduledRuns.runsTasksReadOnlyAt")}>
      <div className="box">
        {unreadable}
        <div className="lrow">
          <span className="tx">
            <span className="lb">{board.pausedAll ? t("scheduledRuns.allScheduledRunsArePaused") : t("scheduledRuns.scheduledRunsAreOn")}</span>
            <span className="ds">{t("scheduledRuns.atMostAtLeastMinutes", { n: board.policy.maxSchedules, m: board.policy.minIntervalMinutes, k: board.policy.globalTokensDay.toLocaleString() })}</span>
          </span>
          {board.pausedAll ? (
            <button className="act" data-action="schedule.resume-all" disabled={locked} onClick={() => void run(() => port.setSchedulesPaused(false))}>{t("scheduledRuns.resumeAll")}</button>
          ) : (
            <button className="act" data-action="schedule.pause-all" disabled={busy} onClick={() => void run(() => port.setSchedulesPaused(true))}>{t("scheduledRuns.pauseAll")}</button>
          )}
        </div>

        {board.schedules.length === 0 && <div className="peernote">{t("scheduledRuns.noScheduledRunsYet")}</div>}
        <ul className="wa-list" aria-label={t("scheduledRuns.scheduledRuns")}>
          {board.schedules.map((s) => (
            <li key={s.id}>
              <span className="tx">
                <span className="lb">{name(s)}</span>
                <span className="ds">{trigger(s)} · {t(STATUS[s.status] ?? s.status)}{s.pausedReason ? " · " + t(PAUSE[s.pausedReason] ?? s.pausedReason) : ""}</span>
                <span className="ds">{s.workspace} · {s.model}</span>
                <span className="ds">{t("scheduledRuns.usedOfTokensEnds", { a: s.spentLifetimeTokens.toLocaleString(), b: s.lifetimeTokens.toLocaleString(), d: when(s.expiresAtUnix) })}</span>
              </span>
              <span className="acts">
                {s.status === "active" ? (
                  <button className="act" data-action="schedule.pause" data-target={s.id} disabled={busy} onClick={() => void run(() => port.scheduleAction(s.id, "pause"))}>{t("common.pause")}</button>
                ) : (
                  <button className="act" data-action="schedule.resume" data-target={s.id} disabled={locked} onClick={() => void run(() => port.scheduleAction(s.id, "resume"))}>{t("common.resume")}</button>
                )}
                <button className="act" data-action="schedule.renew" data-target={s.id} disabled={locked} onClick={() => void run(() => port.scheduleAction(s.id, "renew"))}>{t("scheduledRuns.renew")}</button>
                {confirmDelete === s.id ? (
                  <button className="act danger" data-action="schedule.delete" data-target={s.id} disabled={busy} onClick={() => void run(async () => { await port.scheduleAction(s.id, "delete"); setConfirmDelete(""); })}>{t("common.confirmDelete")}</button>
                ) : (
                  <button className="act ghost" data-action="schedule.delete-ask" data-target={s.id} disabled={busy} onClick={() => setConfirmDelete(s.id)}>{t("common.delete")}</button>
                )}
              </span>
            </li>
          ))}
        </ul>

        {board.runs.length > 0 && (
          <>
            <div className="peernote"><b>{t("scheduledRuns.recentRuns")}</b></div>
            <ul className="wa-list" aria-label={t("scheduledRuns.recentRuns")}>
              {board.runs.slice(0, 8).map((r) => (
                <li key={r.triggerId}>
                  <span className="tx">
                    <span className="lb">{scheduleName(r)} · {t(RUN[r.state] ?? r.state)}</span>
                    <span className="ds">{when(r.slotUnix)} · {r.chargedTokens.toLocaleString()} {t("scheduledRuns.tokens")}{r.skipReason ? " · " + t(SKIP[r.skipReason] ?? r.skipReason) : ""}</span>
                  </span>
                  {r.state !== "skipped" && r.state !== "claimed" && (
                    <button className="act" data-action="schedule.result" data-target={r.triggerId} disabled={busy} onClick={() => void run(async () => setResult(await port.scheduleResult(r.triggerId)))}>{t("scheduledRuns.viewResult")}</button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}

        {result && (
          <div className="peerform" role="region" aria-label={t("scheduledRuns.result")}>
            <div className="peernote"><b>{t("scheduledRuns.result")}</b> · {when(result.recordedUnix)}</div>
            <div className="peernote">{t("scheduledRuns.theTextBelowWasWritten")}</div>
            <pre className="peerbody" data-testid="schedule-report">{result.report}</pre>
            {result.reportTruncated && <div className="peernote">{t("scheduledRuns.theReportWasTooLong")}</div>}
            {result.pending.length > 0 && (
              <>
                <div className="peernote"><b>{t("scheduledRuns.waitingForYourDecision")}</b></div>
                <div className="peernote">{t("scheduledRuns.theTextBelowWasWritten")}</div>
                <ul className="wa-list" data-testid="schedule-pending">
                  {result.pending.map((p, i) => (
                    <li key={i}><span className="tx"><span className="lb">{p.summary}</span>{p.detail && <span className="ds">{p.detail}</span>}</span></li>
                  ))}
                </ul>
                <div className="peernote">{t("scheduledRuns.noneOfTheseWasRun")}</div>
              </>
            )}
            <span className="acts"><button className="act ghost" data-action="schedule.result-close" onClick={() => setResult(null)}>{t("common.close")}</button></span>
          </div>
        )}

        {!form && (
          <div className="peerform">
            <span className="acts">
              <button className="act" data-action="schedule.new" disabled={locked || board.schedules.length >= board.policy.maxSchedules}
                onClick={() => { setError(""); setPreview(null); setForm({ label: "", everySec: 86400, workspace: "", prompt: "" }); }}>{t("scheduledRuns.newScheduledRun")}</button>
            </span>
          </div>
        )}

        {form && !preview && (
          <div className="peerform" role="group" aria-label={t("scheduledRuns.newScheduledRun")}>
            <label className="peernote" htmlFor="sched-folder">{t("scheduledRuns.folderItCanOnlyRead")}</label>
            {board.current?.trustable && (
              <div className="peernote">
                {board.current.trust === "declined"
                  ? t("scheduledRuns.theOpenProjectWasDeclined", { name: board.current.name })
                  : t("scheduledRuns.theOpenProjectIsNot", { name: board.current.name })}
                {" "}
                <button className="act" data-action="workspace.trust" data-value="trusted" disabled={busy} onClick={() => void run(() => port.decideWorkspaceTrust("trusted"))}>{t("common.trustThisFolder")}</button>
              </div>
            )}
            {board.folders.length === 0 ? (
              !board.current?.trustable && <div className="peernote" id="sched-folder">{t("scheduledRuns.thereIsNoTrustedProject")}</div>
            ) : (
              <select id="sched-folder" className="peerfield" data-action-change="schedule.folder" value={form.workspace} disabled={busy}
                onChange={(e) => draft({ workspace: e.target.value })}>
                <option value="">{t("scheduledRuns.chooseATrustedProject")}</option>
                {board.folders.map((f) => <option key={f.path} value={f.path}>{f.name} — {f.path}</option>)}
              </select>
            )}
            <label className="peernote" htmlFor="sched-prompt">{t("scheduledRuns.whatToDo")}</label>
            <textarea id="sched-prompt" className="peerfield" data-action-change="schedule.prompt" value={form.prompt} disabled={busy}
              onChange={(e) => draft({ prompt: e.target.value })} />
            <label className="peernote" htmlFor="sched-label">{t("scheduledRuns.nameOptional")}</label>
            <input id="sched-label" className="peerfield" data-action-change="schedule.label" value={form.label} disabled={busy} onChange={(e) => draft({ label: e.target.value })} />
            <label className="peernote" htmlFor="sched-every">{t("scheduledRuns.howOften")}</label>
            <select id="sched-every" className="peerfield" data-action-change="schedule.every" value={form.everySec ?? 86400} disabled={busy}
              onChange={(e) => draft({ everySec: Number(e.target.value) })}>
              {EVERY.map(([sec, label]) => <option key={sec} value={sec} disabled={sec < board.policy.minIntervalMinutes * 60}>{t(label)}</option>)}
            </select>
            <span className="acts">
              <button className="act ghost" data-action="schedule.cancel" disabled={busy} onClick={() => setForm(null)}>{t("common.cancel")}</button>
              <button className="act" data-action="schedule.preview" data-primary="" disabled={busy || !form.workspace.trim() || !form.prompt.trim()}
                onClick={() => void run(async () => setPreview(await port.previewSchedule({ ...form, workspace: form.workspace.trim(), prompt: form.prompt.trim() })))}>{t("common.preview")}</button>
            </span>
          </div>
        )}

        {form && preview && (
          <div className="peerform" role="group" aria-label={t("scheduledRuns.confirmAndCreate")}>
            <div className="peernote"><b>{t("scheduledRuns.pleaseConfirm")}</b></div>
            <ul className="wa-list">
              <li><span className="tx"><span className="lb">{t("scheduledRuns.firstRun")}</span><span className="ds">{when(preview.firstRunUnix)}</span></span></li>
              <li><span className="tx"><span className="lb">{t("scheduledRuns.folder")}</span><span className="ds">{preview.workspace} {t("scheduledRuns.readOnlyItCanRead")}</span></span></li>
              <li><span className="tx"><span className="lb">{t("common.models")}</span><span className="ds">{preview.model}</span></span></li>
              <li><span className="tx"><span className="lb">{t("scheduledRuns.whatToDo")}</span><span className="ds">{preview.prompt}</span></span></li>
              <li><span className="tx"><span className="lb">{t("scheduledRuns.eachRunAtMost")}</span><span className="ds">{t("scheduledRuns.tokensStepsS", { a: preview.perRunTokens.toLocaleString(), b: preview.perRunSteps, c: preview.perRunWallSec })}</span></span></li>
              <li><span className="tx"><span className="lb">{t("scheduledRuns.inTotalAtMost")}</span><span className="ds">{t("scheduledRuns.tokensADayInAll", { a: preview.perDayMax.toLocaleString(), b: preview.lifetimeTokens.toLocaleString(), d: when(preview.expiresAtUnix) })}</span></span></li>
            </ul>
            <div className="peernote">{t("scheduledRuns.anythingThatNeedsYourApproval")}</div>
            <span className="acts">
              <button className="act ghost" data-action="schedule.back" disabled={busy} onClick={() => setPreview(null)}>{t("scheduledRuns.back")}</button>
              <button className="act" data-action="schedule.create" data-primary="" disabled={locked}
                onClick={() => void run(async () => { await port.createSchedule({ ...form, workspace: form.workspace.trim(), prompt: form.prompt.trim() }, preview.confirm); setForm(null); setPreview(null); })}>{t("scheduledRuns.confirmAndCreate")}</button>
            </span>
          </div>
        )}

        {readOnly && <div className="peernote" role="status">{t("common.theseSettingsAreReadOnly")}</div>}
        {error && <div className="why" role="alert">{error}</div>}
      </div>
    </Group>
  );
}
