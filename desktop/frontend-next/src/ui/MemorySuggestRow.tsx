import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { MemorySuggestion } from "../port/memory";
import { StudioIcon } from "./StudioIcon";

const SCOPE: Record<string, string> = { project: "chat.remember_scope_project", global: "chat.remember_scope_global" };

// The facts the model proposed to keep, one line each above the composer.
// The kernel holds them until the user decides; the list is read again each
// time a turn ends, which is the only moment a new one can have appeared.
export function MemorySuggestRow({ port, running, onError }: { port: AgentPort; running: boolean; onError: (e: unknown) => void }) {
  const [items, setItems] = useState<MemorySuggestion[]>([]);
  const [editing, setEditing] = useState("");
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => {
    if (running) return;
    let live = true;
    port.memorySuggestions().then((got) => { if (live) setItems(got); }).catch(() => {});
    return () => { live = false; };
  }, [port, running]);

  const settle = useCallback(async (id: string, act: () => Promise<void>) => {
    setBusy(id);
    try {
      await act();
      setItems((was) => was.filter((s) => s.id !== id));
      setEditing("");
    } catch (e) {
      onError(new Error(reason(e)));
    } finally {
      setBusy("");
    }
  }, [onError]);

  if (items.length === 0) return null;
  return (
    <div className="msug">
      {items.map((s) => (
        <div className="msug-item" key={s.id}>
          <div className="msug-line">
            <StudioIcon name="sparkle" />
            <span className="msug-lead">{t("chat.suggest_lead")}</span>
            <b>{s.title || s.description}</b>
            <span className="msug-meta">{`· ${t(SCOPE[s.scope ?? "project"] ?? "chat.remember_scope_project")}`}</span>
          </div>
          {editing === s.id ? (
            <div className="msug-edit">
              <textarea data-action="memory.suggest-text" aria-label={t("chat.suggest_text")} value={draft} onChange={(e) => setDraft(e.target.value)} rows={3} />
              <div className="msug-acts">
                <button type="button" data-primary data-action="memory.suggest-save" disabled={busy === s.id || !draft.trim()}
                  onClick={() => void settle(s.id, () => port.acceptMemorySuggestion(s.id, { body: draft }))}>{t("chat.suggest_remember")}</button>
                <button type="button" data-action="memory.suggest-cancel" onClick={() => setEditing("")}>{t("common.cancel")}</button>
              </div>
            </div>
          ) : (
            <div className="msug-acts">
              <button type="button" data-primary data-action="memory.suggest-accept" disabled={busy === s.id}
                onClick={() => void settle(s.id, () => port.acceptMemorySuggestion(s.id))}>{t("chat.suggest_remember")}</button>
              <button type="button" data-action="memory.suggest-edit" disabled={busy === s.id} onClick={() => { setDraft(s.body); setEditing(s.id); }}>{t("chat.suggest_edit")}</button>
              <button type="button" data-action="memory.suggest-dismiss" disabled={busy === s.id}
                onClick={() => void settle(s.id, () => port.dismissMemorySuggestion(s.id))}>{t("chat.suggest_dismiss")}</button>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
