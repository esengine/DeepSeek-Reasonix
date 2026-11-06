import { useCallback, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import type { WireEvent } from "../port/wire";
import { useDismiss } from "./dismiss";

const tokenLabel = (n: number): string =>
  n >= 1_000_000
    ? `${(n / 1_000_000).toFixed(1)}M`
    : n >= 1_000
      ? `${Math.round(n / 100) / 10}K`
      : String(n);

export interface CompactAction {
  busy: boolean;
  pending: boolean;
  run: () => void;
}

export function useCompactNow(
  port: AgentPort,
  blocked: boolean,
  onError: (error: unknown) => void,
) {
  const [pending, setPending] = useState(false);
  const held = useRef(false);
  const release = useCallback(() => {
    held.current = false;
    setPending(false);
  }, []);
  const onEvent = useCallback(
    (event: WireEvent) => {
      if (
        event.kind === "notice" &&
        (event.code === "compacted" ||
          event.code === "compact_declined" ||
          event.code === "compact_failed")
      )
        release();
    },
    [release],
  );
  const run = useCallback(() => {
    if (blocked || held.current) return;
    held.current = true;
    setPending(true);
    // /submit acknowledges admission; the typed notice settles the operation.
    void port.submit("/compact").catch((error) => {
      release();
      onError(error);
    });
  }, [port, blocked, onError, release]);
  return useMemo(
    () => ({ busy: blocked || pending, pending, run, onEvent, release }),
    [blocked, pending, run, onEvent, release],
  );
}

export function CompactNow({
  action,
  usage,
}: {
  action: CompactAction;
  usage?: { used: number; window: number };
}) {
  const [asking, setAsking] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const close = useCallback(() => setAsking(false), []);
  useDismiss(asking, box, close);
  const cancel = () => {
    close();
    trigger.current?.focus();
  };
  return (
    <div className="compact-now" ref={box}>
      <button
        ref={trigger}
        className="btn sm"
        data-action="compaction.confirm"
        disabled={action.busy}
        aria-expanded={asking}
        onClick={() => setAsking(true)}
      >
        {action.pending ? t("common.compacting") : t("composer.compact_now")}
      </button>
      {asking && (
        <div
          className="wsconfirm"
          role="alertdialog"
          aria-label={t("composer.compact_the_current_context")}
        >
          <div className="wsconfirm-t">
            <span className="q">
              {t("composer.compact_the_current_context")}
            </span>
            <span className="h">
              {t("composer.the_model_will_continue_from")}
            </span>
            {usage && usage.window > 0 && (
              <span className="u">
                {t("composer.in_use_now")} {tokenLabel(usage.used)}
                <small>
                  {" "}
                  · {t("common.model_capacity")} {tokenLabel(usage.window)}
                </small>
              </span>
            )}
          </div>
          <div className="wsconfirm-a">
            <button autoFocus data-action="layer.dismiss" onClick={cancel}>
              {t("common.cancel")}
            </button>
            <button
              data-action="compaction.now"
              disabled={action.busy}
              onClick={() => {
                close();
                action.run();
                trigger.current?.focus();
              }}
            >
              {t("composer.confirm_compaction")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
