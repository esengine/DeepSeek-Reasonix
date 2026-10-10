import { useCallback, useLayoutEffect, useMemo, useState } from "react";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import type { AgentPort, RewindResult, RewindScope } from "../port/port";

export type ReloadFailure = { error: string; working: boolean };
export type RestoreNotice = { tx: string; files: number; working: boolean; error: string };

/** A restore receipt belongs to the conversation, not to a transcript card that
 *  the restore itself can remove. Session changes retire pending callbacks too. */
export function useRewindActions(port: AgentPort, reloadSession: (current?: () => boolean) => void | Promise<void>, onRestoreText: (text: string) => void, session = "", running = false) {
  const owner = useMemo(() => ({ active: true, epoch: 0, run: 0, notice: null as RestoreNotice | null,
    commit: null as { planId: string; promise: Promise<RewindResult> } | null, undo: null as Promise<void> | null, reload: null as Promise<void> | null }), [port, session]);
  const [notice, setNotice] = useState<{ owner: typeof owner; value: RestoreNotice | null } | null>(null);
  const [reloadFailure, setReloadFailure] = useState<(ReloadFailure & { owner: typeof owner }) | null>(null);
  useLayoutEffect(() => {
    owner.active = true;
    return () => { owner.active = false; owner.epoch++; };
  }, [owner]);
  const publish = useCallback((value: RestoreNotice | null) => {
    owner.notice = value;
    setNotice({ owner, value });
  }, [owner]);
  // A new run consumes kernel undo, but a completed mutation still needs its history read.
  useLayoutEffect(() => {
    if (running) { owner.run++; publish(null); }
  }, [owner, running, publish]);
  const onReloadSession = useCallback(() => {
    if (!owner.active) return Promise.resolve();
    if (owner.reload) return owner.reload;
    const epoch = owner.epoch;
    const current = () => owner.active && owner.epoch === epoch;
    setReloadFailure((prev) => prev?.owner === owner ? { ...prev, working: true } : prev);
    const operation = (async () => {
      try {
        await reloadSession(current);
        if (current()) setReloadFailure(null);
      } catch (e) {
        if (current()) setReloadFailure({ owner, error: reason(e), working: false });
      }
    })();
    owner.reload = operation;
    void operation.finally(() => { if (owner.reload === operation) owner.reload = null; });
    return operation;
  }, [owner, reloadSession]);
  const onPrepareRewind = useCallback(async (turn: number, scope: RewindScope) => {
    if (!owner.active || owner.commit || owner.undo || owner.reload) throw new Error(t("正在还原…"));
    const epoch = owner.epoch;
    const run = owner.run;
    const plan = await port.prepareRewind(turn, scope);
    if (!owner.active || owner.epoch !== epoch || owner.run !== run) throw new Error(t("会话已切换，请重新选择还原位置"));
    return plan;
  }, [port, owner]);
  const onCommitRewind = useCallback((planId: string, text?: string) => {
    if (!owner.active) return Promise.reject(new Error(t("会话已切换，请重新选择还原位置")));
    if (owner.commit) return owner.commit.planId === planId ? owner.commit.promise : Promise.reject(new Error(t("正在还原…")));
    if (owner.undo || owner.reload) return Promise.reject(new Error(t("正在还原…")));
    const epoch = owner.epoch;
    const run = owner.run;
    const previous = owner.notice;
    if (previous) publish({ ...previous, working: true, error: "" });
    const operation = (async () => {
      const result = await port.commitRewind(planId);
      if (!owner.active || owner.epoch !== epoch) return result;
      const tx = result.undoAvailable ? result.transactionId : undefined;
      if (owner.run === run) {
        publish(tx ? { tx, files: (result.written?.length ?? 0) + (result.deleted?.length ?? 0), working: true, error: "" } : null);
        if (result.conversationOk && text !== undefined) onRestoreText(text);
      }
      await onReloadSession();
      if (owner.active && owner.epoch === epoch && tx && owner.notice && owner.notice.tx === tx) publish({ ...owner.notice, working: false });
      return result;
    })();
    owner.commit = { planId, promise: operation };
    void operation.catch(() => {
      if (owner.active && owner.epoch === epoch && owner.run === run) publish(previous);
    }).finally(() => {
      if (owner.commit?.promise === operation) owner.commit = null;
    });
    return operation;
  }, [port, owner, publish, onReloadSession, onRestoreText]);
  const onUndoRewind = useCallback((transactionId: string) => {
    if (!owner.active || owner.notice?.tx !== transactionId) return Promise.resolve();
    if (owner.undo) return owner.undo;
    if (owner.commit || owner.reload) return Promise.resolve();
    const receipt = owner.notice;
    const epoch = owner.epoch;
    publish({ ...receipt, working: true, error: "" });
    const operation = (async () => {
      try {
        await port.undoRewind(transactionId);
        if (!owner.active || owner.epoch !== epoch) return;
        if (owner.notice?.tx === transactionId) publish(null);
        await onReloadSession();
      } catch (e) {
        if (owner.active && owner.epoch === epoch && owner.notice?.tx === transactionId) {
          publish({ ...receipt, working: false, error: reason(e) });
        }
      }
    })();
    owner.undo = operation;
    void operation.finally(() => { if (owner.undo === operation) owner.undo = null; });
    return operation;
  }, [port, owner, publish, onReloadSession]);
  const dismissRestore = useCallback(() => { if (!owner.notice?.working) publish(null); }, [owner, publish]);
  const onPrepareFileRevert = useCallback((path: string) => port.prepareFileRevert(path), [port]);
  const onCommitFileRevert = useCallback(async (planId: string, resolution?: string) => {
    const epoch = owner.epoch;
    const result = await port.commitFileRevert(planId, resolution);
    if (owner.active && owner.epoch === epoch && result.transactionId) publish(null);
    return result;
  }, [port, owner, publish]);
  return { reloadFailure: reloadFailure?.owner === owner ? reloadFailure : null, onReloadSession, restoreNotice: notice?.owner === owner ? notice.value : null, dismissRestore, onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert };
}
