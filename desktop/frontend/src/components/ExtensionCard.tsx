import { useState } from "react";
import { Puzzle } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useToast } from "../lib/toast";
import type { ExtensionItem } from "../lib/useController";
import { Markdown } from "./Markdown";

// ExtensionCard renders one extension-published card surface in the
// transcript: title, markdown body (via the shared safe renderer), a key-value
// grid, an optional progress bar, and the action buttons declared in the
// extension's handshake. Action results surface inline and, when the extension
// returned a message, as a toast.
export function ExtensionCard({ item, tabId }: { item: ExtensionItem; tabId?: string }) {
  const t = useT();
  const { showToast } = useToast();
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [result, setResult] = useState<{ text: string; error: boolean } | null>(null);
  const { card } = item;
  const progress = typeof card.progress === "number" ? Math.max(0, Math.min(1, card.progress)) : undefined;

  const invoke = async (actionId: string) => {
    if (busyAction) return;
    setBusyAction(actionId);
    try {
      const message = await app.InvokeExtensionAction(tabId ?? "", `/${item.pluginId}:${actionId}`, {});
      const text = message || t("ext.action.done");
      setResult({ text, error: false });
      if (message) showToast(message, "info");
    } catch (err) {
      const text = err instanceof Error ? err.message : String(err);
      setResult({ text, error: true });
      showToast(text, "error");
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="extension-card" data-entrance={item.id} data-extension-surface={item.surfaceKey}>
      <div className="extension-card__header">
        <Puzzle size={14} className="extension-card__icon" aria-hidden="true" />
        {card.title ? <span className="extension-card__title">{card.title}</span> : null}
        <span className="extension-card__plugin">{item.pluginId}</span>
      </div>
      {card.markdown ? (
        <div className="extension-card__body">
          <Markdown text={card.markdown} />
        </div>
      ) : null}
      {!card.markdown && card.text ? <div className="extension-card__body">{card.text}</div> : null}
      {card.fields && card.fields.length > 0 ? (
        <dl className="extension-card__fields">
          {card.fields.map((field, index) => (
            <div className="extension-card__field" key={`${field.key}-${index}`}>
              <dt>{field.key}</dt>
              <dd>{field.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      {progress !== undefined ? (
        <div
          className="extension-card__progress"
          role="progressbar"
          aria-label={t("ext.card.progress")}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(progress * 100)}
        >
          <div className="extension-card__progress-fill" style={{ width: `${Math.round(progress * 100)}%` }} />
        </div>
      ) : null}
      {card.actions && card.actions.length > 0 ? (
        <div className="extension-card__actions">
          {card.actions.map((action) => (
            <button
              key={action.actionId}
              type="button"
              className="btn btn--small"
              disabled={busyAction !== null}
              onClick={() => void invoke(action.actionId)}
            >
              {action.label || action.actionId}
            </button>
          ))}
        </div>
      ) : null}
      {result ? (
        <div className={`extension-card__result${result.error ? " extension-card__result--error" : ""}`}>{result.text}</div>
      ) : null}
    </div>
  );
}
