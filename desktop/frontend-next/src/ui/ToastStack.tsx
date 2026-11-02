import { useLayoutEffect, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, Appearance } from "../port/port";
import { dismissToast, useToasts } from "../state/toasts";
import { useUpdateReady } from "../state/updatewatch";
import { reportProblem } from "./reportproblem";
import { useSizeAsk } from "./SizeAsk";
import { UpdateReady } from "./UpdateReady";

/** The window's corner for failures that do not block what the person is doing.
 *  They stay until dismissed, and each can be reported from where it appeared. */
export function ToastStack({ onFeedback, port, idle = false, look = {}, onLook = () => {} }: { onFeedback: () => void; port?: Pick<AgentPort, "restartToVersion">; idle?: boolean; look?: Appearance; onLook?: (next: Appearance) => void }) {
  const toasts = useToasts();
  const waiting = useUpdateReady() !== "";
  const update = port !== undefined && waiting && idle;
  const ask = useSizeAsk(look, onLook);
  const at = useAboveComposer(toasts.length > 0 || update || ask.open);
  if (toasts.length === 0 && !update && !ask.open) return null;
  return (
    <div className="toasts" role="region" aria-label={t("通知")} style={at ? { bottom: at.bottom, right: at.right } : undefined}>
      {ask.node}
      {port && <UpdateReady port={port} idle={idle} />}
      {toasts.map((toast) => toast.tone === "note" ? (
        <div className="toast note" role="status" key={toast.id}>
          <span>{toast.text}</span>
          {toast.action && <button data-action="toast.act" data-target={String(toast.id)} onClick={() => { dismissToast(toast.id); toast.action?.run(); }}>{toast.action.label}</button>}
          <button data-action="toast.dismiss" data-target={String(toast.id)} onClick={() => dismissToast(toast.id)}>{t("知道了")}</button>
        </div>
      ) : (
        <div className="toast" role="alert" key={toast.id}>
          <span>{toast.text}</span>
          <button data-action="toast.report" onClick={() => { reportProblem(toast.text); onFeedback(); dismissToast(toast.id); }}>{t("报告此问题")}</button>
          <button data-action="toast.dismiss" data-target={String(toast.id)} onClick={() => dismissToast(toast.id)}>{t("知道了")}</button>
        </div>
      ))}
    </div>
  );
}

// Where the corner notices sit: just above the composer of the pane in front,
// lined up with its right edge, so they never cover the box's own controls
// (the model picker, send) at any width. With no composer on screen they keep
// the window's corner. Measured, because the composer moves with the window,
// its own height and the pane in front.
function useAboveComposer(on: boolean): { bottom: number; right: number } | null {
  const [at, setAt] = useState<{ bottom: number; right: number } | null>(null);
  useLayoutEffect(() => {
    if (!on) return;
    const measure = () => {
      const box = [...document.querySelectorAll<HTMLElement>(".compose")].find((el) => el.getClientRects().length > 0);
      const r = box?.getBoundingClientRect();
      const next = r ? { bottom: Math.round(innerHeight - r.top + 8), right: Math.max(16, Math.round(innerWidth - r.right)) } : null;
      setAt((cur) => (cur?.bottom === next?.bottom && cur?.right === next?.right ? cur : next));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(document.body);
    const box = [...document.querySelectorAll<HTMLElement>(".compose")].find((el) => el.getClientRects().length > 0);
    if (box) observer.observe(box);
    addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      removeEventListener("resize", measure);
    };
  }, [on]);
  return at;
}
