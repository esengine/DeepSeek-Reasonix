import { purgeTrashBatch } from "../lib/trashOperations";
import { useCallback, useEffect, useRef, useState } from "react";
import { RotateCw } from "lucide-react";
import { app } from "../lib/bridge";
import type { SessionMeta } from "../lib/types";
import { useT } from "../lib/i18n";
import { useManagementT } from "../lib/managementLocale";
import { ManagementPageShell } from "./ManagementPageShell";
import { HistoryPanel } from "./HistoryPanel";
import { useConfirmDialog } from "./ConfirmDialog";
import "./TrashPage.css";

const noop = () => {};
export function TrashPage({ active, onBack, list, restore, purge }: {
  active: boolean; onBack: () => void; list: () => Promise<SessionMeta[]>;
  restore: (path: string) => Promise<void>; purge: (path: string) => Promise<void>;
}) {
  const t = useT(); const m = useManagementT();
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [failedPaths, setFailedPaths] = useState<string[]>([]);
  const busyRef = useRef(false); const seq = useRef(0);
  const { confirm, dialog, dismiss } = useConfirmDialog();
  useEffect(() => { if (!active) dismiss(); }, [active, dismiss]);
  const refresh = useCallback(async (afterMutation = false) => {
    const generation = ++seq.current;
    setLoading(true);
    try {
      const value = await list();
      if (generation !== seq.current) return;
      setSessions(value); setLoadFailed(false);
    } catch {
      if (generation !== seq.current) return;
      setLoadFailed(true);
      if (afterMutation) setNotice(m("refreshedFailed"));
    } finally { if (generation === seq.current) setLoading(false); }
  }, [list, m]);
  useEffect(() => { if (active && !busyRef.current) void refresh(); }, [active, refresh]);
  useEffect(() => () => { seq.current++; }, []);
  const mutate = async (paths: string[], kind: "restore" | "purge") => {
    if (busyRef.current) return;
    busyRef.current = true; setBusy(true); setLastKind(kind); ++seq.current; setFailedPaths([]);
    try {
      const result = await purgeTrashBatch(paths, kind === "restore" ? restore : purge);
      const removed = new Set(result.succeeded);
      setSessions((current) => current.filter((item) => !removed.has(item.path)));
      setFailedPaths(result.failed);
      setNotice(paths.length > 1 ? m("batchResult", { success: result.succeeded.length, failed: result.failed.length })
        : result.failed.length ? m("operationFailed") : m(kind === "restore" ? "restored" : "deleted"));
      await refresh(result.succeeded.length > 0);
    } finally { busyRef.current = false; setBusy(false); }
  };
  const requestPurge = async (paths: string[], clear: boolean) => {
    if (busyRef.current) return;
    const snapshot = [...new Set(paths)];
    const name = sessions.find((item) => item.path === snapshot[0]);
    if (await confirm({ title: m(clear ? "clearTitle" : "purgeTitle"),
      message: clear ? m("clearDescription", { n: snapshot.length }) : m("purgeDescription", { name: name?.topicTitle || name?.title || name?.preview || t("history.emptySession") }),
      confirmLabel: m("confirm"), cancelLabel: m("cancel"), tone: "danger" })) await mutate(snapshot, "purge");
  };
  const [lastKind, setLastKind] = useState<"restore" | "purge">("purge");
  return <ManagementPageShell active={active} onBack={onBack} title={`${t("history.trashTitle")} · ${sessions.filter((item) => !item.recoveryCopy).length}`}
    description={m("trashDescription")} actions={<><button className="btn btn--small" disabled={busy || loading} onClick={() => void refresh()}><RotateCw size={14} />{m("refresh")}</button><button className="btn btn--small btn--danger history-clear" disabled={busy || !sessions.some((item) => !item.recoveryCopy)} onClick={() => void requestPurge(sessions.filter((item) => !item.recoveryCopy).map((item) => item.path), true)}>{t("history.clearTrash")}</button></>}>
    {loadFailed && <div className="management-notice" role="alert">{m("loadFailed")}<button className="btn btn--small" disabled={busy} onClick={() => void refresh()}>{m("retry")}</button></div>}
    {notice && <div className="management-notice" role="status">{notice}{failedPaths.length > 0 && <button className="btn btn--small" disabled={busy} onClick={() => void mutate(failedPaths, lastKind)}>{m("retryFailed")}</button>}</div>}
    {loading && <div className="management-notice" role="status">{m("loading")}</div>}
    <div style={{ display: sessions.length === 0 && (loading || loadFailed) ? "none" : "contents" }}><HistoryPanel presentation="page" active={active} sessions={sessions} kind="trash" running={false} busy={busy}
      onClose={onBack} onResume={noop} onDelete={noop} onRename={noop}
      onPreview={async (path) => (await app.PreviewSession(path)) ?? []}
      onRestore={async (path) => { await mutate([path], "restore"); }}
      onPurge={async (path) => { await requestPurge([path], false); }}
      onPurgeAll={async (paths) => { await requestPurge(paths, true); }} /></div>
    {active && dialog}
  </ManagementPageShell>;
}
