import { Check, Monitor, Server } from "lucide-react";
import { useEffect, useState } from "react";
import type { SessionRuntime } from "../protocol/types";
import type { PairedNode } from "../lib/paired-nodes";
import { t, type Locale } from "../i18n/messages";
import { BottomSheet } from "./BottomSheet";

const DEFAULT_NODE = "http://127.0.0.1:8790";

export function NewSessionSheet({
  open,
  locale,
  busy,
  error,
  pairedNodes,
  onClose,
  onCreate,
}: {
  open: boolean;
  locale: Locale;
  busy: boolean;
  error: string | null;
  pairedNodes: PairedNode[];
  onClose: () => void;
  onCreate: (input: { runtime: SessionRuntime; nodeUrl?: string }) => void;
}) {
  const [runtime, setRuntime] = useState<SessionRuntime>("local");
  const [nodeUrl, setNodeUrl] = useState(DEFAULT_NODE);

  useEffect(() => {
    if (!open) return;
    setRuntime("local");
    setNodeUrl(pairedNodes[0]?.baseUrl || DEFAULT_NODE);
  }, [open, pairedNodes]);

  return (
    <BottomSheet
      open={open}
      title={t(locale, "sessions.pickRuntime")}
      description={t(locale, "sessions.pickRuntimeDesc")}
      localeCloseLabel={t(locale, "common.close")}
      onClose={onClose}
    >
      <div className="anim-enter">
        <button
          type="button"
          className="choice-card"
          aria-pressed={runtime === "local"}
          onClick={() => setRuntime("local")}
        >
          <span className="choice-icon" aria-hidden>
            <Monitor size={20} />
          </span>
          <span>
            <div className="choice-title">{t(locale, "sessions.runtimeLocal")}</div>
            <div className="choice-desc">{t(locale, "sessions.localDesc")}</div>
          </span>
          {runtime === "local" ? <Check size={18} color="var(--rx-accent)" /> : <span />}
        </button>

        <button
          type="button"
          className="choice-card"
          aria-pressed={runtime === "remote"}
          onClick={() => setRuntime("remote")}
        >
          <span className="choice-icon remote" aria-hidden>
            <Server size={20} />
          </span>
          <span>
            <div className="choice-title">{t(locale, "sessions.runtimeRemote")}</div>
            <div className="choice-desc">{t(locale, "sessions.remoteDesc")}</div>
          </span>
          {runtime === "remote" ? <Check size={18} color="var(--rx-accent)" /> : <span />}
        </button>

        {runtime === "remote" && (
          <div className="anim-enter-delayed">
            {pairedNodes.length > 0 ? (
              <div className="sheet-field">
                <label>{t(locale, "nodes.title")}</label>
                <div className="pair-chip-row">
                  {pairedNodes.map((n) => (
                    <button
                      key={n.id}
                      type="button"
                      className={`pair-chip${nodeUrl === n.baseUrl ? " active" : ""}`}
                      onClick={() => setNodeUrl(n.baseUrl)}
                    >
                      <span className="status-dot" data-status={n.online ? "idle" : "failed"} />
                      {n.name}
                    </button>
                  ))}
                </div>
              </div>
            ) : null}
            <div className="sheet-field">
              <label htmlFor="node-url">{t(locale, "sessions.nodeUrl")}</label>
              <input
                id="node-url"
                value={nodeUrl}
                onChange={(e) => setNodeUrl(e.target.value)}
                placeholder={t(locale, "sessions.nodeUrlPlaceholder")}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                inputMode="url"
              />
            </div>
          </div>
        )}

        {error ? <p className="sheet-error">{error}</p> : null}

        <div className="sheet-actions">
          <button
            type="button"
            className="btn-primary"
            disabled={busy || (runtime === "remote" && !nodeUrl.trim())}
            onClick={() =>
              onCreate({
                runtime,
                nodeUrl: runtime === "remote" ? nodeUrl.trim() : undefined,
              })
            }
          >
            {t(locale, "sessions.create")}
          </button>
          <button type="button" className="btn-secondary" onClick={onClose} disabled={busy}>
            {t(locale, "sessions.cancel")}
          </button>
        </div>
      </div>
    </BottomSheet>
  );
}
