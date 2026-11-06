import { useEffect, useId, useRef, useState } from "react";
import type { AgentPort } from "../port/port";
import type { AgentComposition, AgentPresetCatalog, AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";
import { reason, say } from "../i18n/kernel";
import { CompositionEditor } from "./CompositionEditor";
import { PresetActions } from "./PresetActions";
import { usePresetLeave } from "./PresetLeave";
import { presetChanges, samePreset } from "./presetDraft";
import { StudioIcon } from "./StudioIcon";

export interface PresetEntry {
  definition: AgentComposition;
  version: AgentPresetVersion | null;
  source: string;
  diagnostics?: AgentPresetCatalog["diagnostics"];
}

export function PresetPage({ entry, catalog, port, onClose, onSaved, onDeleted, onCopy, onPersona }: {
  entry: PresetEntry;
  catalog: AgentPresetCatalog;
  port: AgentPort;
  onClose: () => void;
  onSaved: (version: AgentPresetVersion) => void;
  onDeleted: () => void;
  onCopy: (definition: AgentComposition) => void;
  onPersona: () => void;
}) {
  const [draft, setDraft] = useState(() => structuredClone(entry.definition));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [valid, setValid] = useState(true);
  const root = useRef<HTMLFormElement>(null);
  const titleId = useId();
  const immutable = draft.id.startsWith("builtin:");
  const changed = !samePreset(draft, entry.definition);
  const dirty = !immutable && (changed || !valid);
  const leave = usePresetLeave(dirty, busy, presetChanges(draft, entry.definition));
  const title = t(immutable ? "persona.preset_details" : entry.version ? "persona.edit_preset" : "persona.new_preset");
  const canSave = !immutable && !busy && valid && !!draft.name.trim() &&
    /^(user|project):[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/.test(draft.id) && (!entry.version || changed);
  useEffect(() => {
    (root.current?.querySelector<HTMLElement>("input:not(:disabled)") ?? root.current?.querySelector<HTMLElement>("button"))?.focus();
    root.current?.closest(".prefs-main")?.scrollTo?.({ top: 0 });
  }, []);
  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try { await work(); } catch (e) { setError(reason(e)); } finally { setBusy(false); }
  };
  return <>
    <form ref={root} className="preset-page" aria-labelledby={titleId} data-action-submit="agent-preset.save"
      onSubmit={(e) => { e.preventDefault(); if (canSave) void run(async () => onSaved(await port.saveAgentPreset(draft, entry.version?.revision ?? ""))); }}
      data-action-keydown="agent-preset.leave" onKeyDown={(e) => {
        if (e.key !== "Escape") return;
        e.preventDefault(); e.stopPropagation(); leave.request(onClose);
      }}>
      <header className="preset-page-heading">
        <button type="button" className="preset-back" data-action="agent-preset.leave" disabled={busy} onClick={() => leave.request(onClose)}><StudioIcon name="chevron" />{t("persona.back_to_presets")}</button>
        <h2 id={titleId}>{title}</h2>
        <p>{immutable || entry.version ? entry.source : t("persona.based_on_name", { name: entry.source })}</p>
        {entry.version && <small className="preset-revision">{t("persona.preset_version")}: {entry.version.revision.slice(0, 12)}</small>}
      </header>
      <div className="preset-page-content">
        {immutable && <p className="ds">{t("persona.built_in_presets_are_read")}</p>}
        <CompositionEditor draft={draft} modules={catalog.modules} disabled={busy || immutable} creating={!entry.version}
          onValidityChange={setValid} onChange={(next) => { setDraft(next); setError(""); }} />
        {entry.version && !changed && valid && <PresetActions port={port} version={entry.version} busy={busy} run={run}
          onDeleted={onDeleted} onPersona={onPersona} />}
        {entry.diagnostics?.map((d, i) => <p className="ds" key={i}>{say({ code: d.code, params: { module: d.module ?? "", detail: d.detail } })}</p>)}
        {error && <p className="find" data-lvl="err" role="alert">{error}</p>}
      </div>
      <footer className="preset-page-footer">
        <p>{t("persona.available_to_new_conversations_after")}</p>
        <div>
          <button type="button" className="btn" data-action="agent-preset.leave" disabled={busy} onClick={() => leave.request(onClose)}>{t(immutable ? "common.back" : "common.cancel")}</button>
          {immutable ? <button type="button" className="btn" data-primary="" data-action="agent-preset.copy" disabled={busy} onClick={() => onCopy(draft)}>{t("persona.duplicate_as_custom_preset")}</button>
            : <button type="submit" className="btn" data-primary="" data-action="agent-preset.save" disabled={!canSave}>{t(busy ? "common.saving" : "persona.save_preset")}</button>}
        </div>
      </footer>
    </form>
    {leave.confirmation}
  </>;
}
