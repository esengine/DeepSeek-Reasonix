import { AlertTriangle, Check, ShieldAlert, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { ApprovalRequest } from "../lib/approval";
import { t, type Locale } from "../i18n/messages";
import { BottomSheet } from "./BottomSheet";

const HOLD_MS = 900;

export function ApprovalSheet({
  open,
  locale,
  request,
  busy,
  onClose,
  onAllow,
  onDeny,
}: {
  open: boolean;
  locale: Locale;
  request: ApprovalRequest | null;
  busy?: boolean;
  onClose: () => void;
  onAllow: () => void;
  onDeny: () => void;
}) {
  const [holdProgress, setHoldProgress] = useState(0);
  const holdTimer = useRef<number | null>(null);
  const raf = useRef<number | null>(null);
  const startRef = useRef(0);

  const clearHold = useCallback(() => {
    if (holdTimer.current) window.clearTimeout(holdTimer.current);
    if (raf.current) cancelAnimationFrame(raf.current);
    holdTimer.current = null;
    raf.current = null;
    setHoldProgress(0);
  }, []);

  useEffect(() => () => clearHold(), [clearHold]);
  useEffect(() => {
    if (!open) clearHold();
  }, [open, clearHold]);

  if (!request) {
    return (
      <BottomSheet
        open={false}
        title={t(locale, "approval.title")}
        localeCloseLabel={t(locale, "common.close")}
        onClose={onClose}
      >
        {null}
      </BottomSheet>
    );
  }

  const needsHold = request.dangerousWrite;
  const riskLabel =
    request.risk === "high"
      ? t(locale, "approval.riskHigh")
      : request.risk === "medium"
        ? t(locale, "approval.riskMedium")
        : t(locale, "approval.riskLow");

  const startHold = () => {
    if (!needsHold || busy) return;
    startRef.current = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - startRef.current) / HOLD_MS);
      setHoldProgress(p);
      if (p < 1) {
        raf.current = requestAnimationFrame(tick);
      }
    };
    raf.current = requestAnimationFrame(tick);
    holdTimer.current = window.setTimeout(() => {
      clearHold();
      onAllow();
    }, HOLD_MS);
  };

  const endHold = () => {
    if (holdProgress < 1) clearHold();
  };

  return (
    <BottomSheet
      open={open}
      title={t(locale, "approval.title")}
      description={t(locale, "approval.desc")}
      localeCloseLabel={t(locale, "common.close")}
      onClose={onClose}
      wide
    >
      <div className="approval-body anim-enter">
        <div className="approval-risk" data-risk={request.risk}>
          <ShieldAlert size={16} aria-hidden />
          <span>{riskLabel}</span>
          {request.dangerousWrite ? (
            <span className="approval-risk-tag">{t(locale, "approval.dangerous")}</span>
          ) : null}
        </div>

        <div className="approval-meta list-group">
          <div className="approval-meta-row">
            <span className="approval-meta-label">{t(locale, "approval.tool")}</span>
            <span className="approval-meta-value mono">{request.tool}</span>
          </div>
          <div className="approval-meta-row">
            <span className="approval-meta-label">{t(locale, "approval.target")}</span>
            <span className="approval-meta-value">{request.subject}</span>
          </div>
          {request.reason ? (
            <div className="approval-meta-row">
              <span className="approval-meta-label">{t(locale, "approval.reason")}</span>
              <span className="approval-meta-value">{request.reason}</span>
            </div>
          ) : null}
        </div>

        {request.command ? (
          <div className="approval-block">
            <div className="approval-block-label">{t(locale, "approval.command")}</div>
            <pre className="approval-code">{request.command}</pre>
          </div>
        ) : null}

        {request.diff ? (
          <div className="approval-block">
            <div className="approval-block-label">{t(locale, "approval.diff")}</div>
            <pre className="approval-code approval-diff">{request.diff}</pre>
          </div>
        ) : null}

        {needsHold ? (
          <p className="approval-hold-hint">
            <AlertTriangle size={14} aria-hidden />
            {t(locale, "approval.holdHint")}
          </p>
        ) : null}

        <div className="sheet-actions approval-actions">
          {needsHold ? (
            <button
              type="button"
              className="btn-hold-allow"
              disabled={busy}
              onPointerDown={(e) => {
                e.preventDefault();
                startHold();
              }}
              onPointerUp={endHold}
              onPointerLeave={endHold}
              onPointerCancel={endHold}
              onContextMenu={(e) => e.preventDefault()}
              aria-label={t(locale, "approval.holdAllow")}
            >
              <span
                className="btn-hold-fill"
                style={{ transform: `scaleX(${holdProgress})` }}
              />
              <span className="btn-hold-label">
                <Check size={18} />
                {holdProgress > 0.05
                  ? t(locale, "approval.holding")
                  : t(locale, "approval.holdAllow")}
              </span>
            </button>
          ) : (
            <button
              type="button"
              className="btn-primary"
              disabled={busy}
              onClick={onAllow}
            >
              <Check size={18} />
              {t(locale, "approval.allow")}
            </button>
          )}
          <button
            type="button"
            className="btn-deny"
            disabled={busy}
            onClick={onDeny}
          >
            <X size={18} />
            {t(locale, "approval.deny")}
          </button>
        </div>
      </div>
    </BottomSheet>
  );
}
