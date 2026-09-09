import { useRef, useState } from "react";
import { Check, Pencil, X } from "lucide-react";
import { useT } from "../lib/i18n";

export function ConnectionTitle({ label, busy, onSave }: {
  label: string; busy: boolean; onSave: (label: string) => Promise<boolean>;
}) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(label);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(false);
  const pending = useRef(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const close = () => { setEditing(false); setError(false); requestAnimationFrame(() => trigger.current?.focus()); };
  const save = async () => {
    if (pending.current || busy) return;
    pending.current = true; setSaving(true); setError(false);
    try {
      if (await onSave(draft.trim())) close(); else setError(true);
    } catch { setError(true); }
    finally { pending.current = false; setSaving(false); }
  };
  if (!editing) return <button ref={trigger} type="button" className="connection-title" disabled={busy}
    aria-label={`${t("settings.connections.rename")}: ${label}`}
    onClick={() => { setDraft(label); setError(false); setEditing(true); }}>
    {label}<Pencil size={15} aria-hidden="true" />
  </button>;
  return <span className="connection-title-editor">
    <span className="connection-title-editor__row">
      <input className="mem-input" aria-label={t("settings.connections.name")} value={draft}
        ref={input => { if (input) input.focus(); }} disabled={saving || busy}
        onChange={e => setDraft(e.target.value)}
        onKeyDown={e => {
          if (e.nativeEvent.isComposing || e.keyCode === 229) return;
          if (e.key === "Enter") { e.preventDefault(); void save(); }
          if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); if (!saving) close(); }
        }} />
      <button type="button" className="btn provider-icon-action" title={t("common.save")} aria-label={t("common.save")} disabled={saving || busy} onClick={() => void save()}><Check size={16} /></button>
      <button type="button" className="btn provider-icon-action" title={t("common.cancel")} aria-label={t("common.cancel")} disabled={saving} onClick={close}><X size={16} /></button>
    </span>
    {error && <small role="alert">{t("settings.connections.renameFailed")}</small>}
  </span>;
}
