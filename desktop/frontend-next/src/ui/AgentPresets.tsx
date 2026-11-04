import { useEffect, useRef, useState } from "react";
import type { AgentPort } from "../port/port";
import type { AgentComposition, AgentPresetCatalog, AgentPresetVersion, CompositionSummary } from "../port/composition";
import { builtinCompositions } from "../port/composition";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { ApplyNote } from "./Group";
import { CompositionCatalog } from "./CompositionCatalog";
import { CompositionPickerSetting } from "./CompositionPickerSetting";
import { PresetPage, type PresetEntry } from "./PresetPage";
import { PresetPersonaPage } from "./PresetPersonaPage";
import { newPreset } from "./presetDraft";

type EditorTarget = { kind: "preset"; entry: PresetEntry } | { kind: "persona" };
type Editor = EditorTarget & { key: number };

export function AgentPresets({ port, focusPersona = false }: { port: AgentPort; focusPersona?: boolean }) {
  const [catalog, setCatalog] = useState<AgentPresetCatalog | null>(null);
  const [current, setCurrent] = useState<CompositionSummary | null>(null);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [refresh, setRefresh] = useState(0);
  const epoch = useRef(0);
  const root = useRef<HTMLElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const openerAction = useRef("");
  const focusID = useRef("");
  useEffect(() => {
    let active = true;
    setCatalog(null);
    setError("");
    Promise.all([port.agentPresets(), port.agentComposition()]).then(([next, binding]) => {
      if (active) { setCatalog(next); setCurrent(binding); }
    }).catch(e => { if (active) setError(reason(e)); });
    return () => { active = false; ++epoch.current; };
  }, [port, refresh]);
  useEffect(() => {
    if (focusPersona) setEditor({ kind: "persona", key: ++epoch.current });
  }, [focusPersona]);
  useEffect(() => {
    if (editor) return;
    const row = [...(root.current?.querySelectorAll<HTMLElement>("button[data-target]") ?? [])].find(el => el.dataset.target === focusID.current);
    const trigger = root.current?.querySelector<HTMLElement>(`[data-action="${openerAction.current}"]`);
    (row ?? (opener.current?.isConnected ? opener.current : trigger))?.focus();
    focusID.current = "";
  }, [editor]);
  const open = (entry: EditorTarget) => {
    if (!editor) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      openerAction.current = opener.current?.dataset.action ?? "";
      focusID.current = opener.current?.dataset.target ?? "";
    }
    setEditor({ ...entry, key: ++epoch.current });
    setError("");
    setSaved("");
  };
  const copy = (base: AgentComposition, duplicate: boolean) => {
    if (!catalog) return;
    open({ kind: "preset", entry: { definition: newPreset(base, catalog, duplicate), version: null,
      source: base.id.startsWith("builtin:") ? t(base.name) : base.name } });
  };
  const close = () => setEditor(null);
  const savedPreset = (version: AgentPresetVersion) => {
    setCatalog(prev => prev && ({ ...prev, presets: [...prev.presets.filter(v => v.definition.id !== version.definition.id), version] }));
    focusID.current = version.definition.id;
    setSaved(t("persona.saved_new_conversations_use_the"));
    close();
  };
  const importFile = async (file: File) => {
    if (!catalog) return;
    const generation = epoch.current;
    const active = () => root.current !== null && epoch.current === generation;
    try {
      if (file.size > 4 * 1024 * 1024) throw { code: "agent_composition.invalid" };
      const imported = await port.importAgentPreset(JSON.parse(await file.text()) as AgentComposition);
      if (!active()) return;
      open({ kind: "preset", entry: { definition: newPreset(imported.definition, catalog, true), version: null,
        source: t("persona.imported_preset"), diagnostics: imported.diagnostics } });
    } catch (e) { if (active()) setError(reason(e)); }
  };
  return <section ref={root} id="set-agent-presets" data-setting="agent-presets" className="preset-workspace" data-editor={editor ? "" : undefined}>
    {editor?.kind === "persona" ? <PresetPersonaPage key={editor.key} port={port} onClose={close} />
      : editor?.kind === "preset" && catalog ? <PresetPage key={editor.key} entry={editor.entry} catalog={catalog} port={port}
        onClose={close} onSaved={savedPreset} onDeleted={() => {
          setCatalog(prev => prev && ({ ...prev, presets: prev.presets.filter(v => v.definition.id !== editor.entry.definition.id) }));
          setSaved(t("persona.preset_deleted_existing_conversations_keep")); close();
        }} onCopy={d => copy(d, true)} onPersona={() => open({ kind: "persona" })} />
      : <div className="preset-list">
        {catalog ? <>
          <CompositionCatalog catalog={catalog} onCreate={() => copy(builtinCompositions()[0], false)}
            onCopy={v => copy(v.definition, true)} onSelect={v => open({ kind: "preset", entry: { definition: v.definition, version: v,
              source: v.definition.id.startsWith("builtin:") ? t(v.definition.name) : v.definition.name } })} />
          <div className="composition-toolbar">
            <label className="act preset-import">{t("persona.import_a_preset_file_preview")}<input type="file" accept="application/json,.json"
              aria-label={t("persona.import_a_preset_file_preview")} data-action-change="agent-preset.import"
              onChange={e => { const file = e.target.files?.[0]; e.target.value = ""; if (file) void importFile(file); }} /></label>
            <button className="act" data-action="agent-preset.reload" onClick={() => setRefresh(v => v + 1)}>{t("common.reload")}</button>
          </div>
          {current && <p className="ds">{t("common.this_session")}: {current.id.startsWith("builtin:") ? t(current.name) : current.name} · {current.revision.slice(0, 12)} {t(current.started ? "persona.locked" : "persona.switch_before_the_first_message")}</p>}
          {catalog.diagnostics.map((d, i) => <p className="ds" key={i}>{d.module}: {d.detail}</p>)}
          <ApplyNote id="agent-presets" />
        </> : <><CompositionPickerSetting /><div className="empty"><p>{error || t("persona.loading_presets")}</p><button className="act" data-action="agent-preset.reload" onClick={() => setRefresh(v => v + 1)}>{t("common.reload")}</button></div></>}
        {saved && <p className="find" role="status">{saved}</p>}
        {catalog && error && <p className="find" data-lvl="err" role="alert">{error}</p>}
      </div>}
  </section>;
}
