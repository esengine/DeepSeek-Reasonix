import { useCallback, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import { useDismiss } from "./dismiss";
import "../styles/compact-now.css";

export interface CompactAction {
  busy: boolean;
  pending: boolean;
  run: () => void;
}

export function useCompactNow(port: AgentPort, blocked: boolean, onError: (error: unknown) => void) {
  const [pending, setPending] = useState(false);
  const held = useRef(false);
  const release = useCallback(() => { held.current = false; setPending(false); }, []);
  const onEvent = useCallback((event: WireEvent) => {
    if (event.kind === "notice" && (event.code === "compacted" || event.code === "compact_declined" || event.code === "compact_failed")) release();
  }, [release]);
  const run = useCallback(() => {
    if (blocked || held.current) return;
    held.current = true;
    setPending(true);
    // /submit acknowledges admission; the typed notice settles the operation.
    void port.submit("/compact").catch((error) => { release(); onError(error); });
  }, [port, blocked, onError, release]);
  return useMemo(() => ({ busy: blocked || pending, pending, run, onEvent, release }), [blocked, pending, run, onEvent, release]);
}

export function CompactNow({ action }: { action: CompactAction }) {
  const [asking, setAsking] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const close = useCallback(() => setAsking(false), []);
  useDismiss(asking, box, close);
  const cancel = () => { close(); trigger.current?.focus(); };
  return (
    <div className="compact-now" ref={box}>
      <button ref={trigger} className="btn sm" data-action="compaction.confirm" disabled={action.busy} aria-expanded={asking} onClick={() => setAsking(true)}>
        {action.pending ? t("正在压缩…") : t("立即压缩")}
      </button>
      {asking && (
        <div className="wsconfirm" role="alertdialog" aria-label={t("压缩当前上下文？")}>
          <div className="wsconfirm-t"><span className="q">{t("压缩当前上下文？")}</span><span className="h">{t("模型将依据摘要继续工作；完整对话记录会保留。")}</span></div>
          <div className="wsconfirm-a">
            <button autoFocus data-action="layer.dismiss" onClick={cancel}>{t("取消")}</button>
            <button data-action="compaction.now" disabled={action.busy} onClick={() => { close(); action.run(); trigger.current?.focus(); }}>{t("确认压缩")}</button>
          </div>
        </div>
      )}
    </div>
  );
}
