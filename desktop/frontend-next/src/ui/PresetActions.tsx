import { useState } from "react";
import type { AgentPort } from "../port/port";
import type { AgentPresetVersion } from "../port/composition";
import { t } from "../i18n";

export function PresetActions({ port, version, busy, run, onDeleted, onPersona }: {
  port: AgentPort;
  version: AgentPresetVersion;
  busy: boolean;
  run: (work: () => Promise<void>) => Promise<void>;
  onDeleted: () => void;
  onPersona: () => void;
}) {
  const [deleting, setDeleting] = useState(false);
  const [exported, setExported] = useState("");
  const [note, setNote] = useState("");
  const d = version.definition;
  return <details className="composition-extra">
    <summary>{t("common.moreActions")}</summary>
    {d.id === "builtin:general" && <button type="button" className="act" data-action="agent-preset.persona" disabled={busy} onClick={onPersona}>{t("presetActions.editEverydayPartnerPersona")}</button>}
    <div className="composition-toolbar">
      <button type="button" className="act" data-action="agent-preset.export" disabled={busy} onClick={() => void run(async () => {
        setExported(JSON.stringify(await port.exportAgentPreset(d.id), null, 2));
      })}>{t("presetActions.exportFile")}</button>
      {!d.id.startsWith("builtin:") && <button type="button" className="act" data-action="agent-preset.delete-preview" disabled={busy} onClick={() => setDeleting(true)}>{t("presetActions.deletePreset")}</button>}
    </div>
    {exported && <a className="act" download="agent-preset.json"
      href={`data:application/json;charset=utf-8,${encodeURIComponent(exported)}`}>{t("presetActions.downloadPresetJSON")}</a>}
    {deleting && <div className="find" role="group" aria-label={t("presetActions.deletePreset")}>
      <p>{t("presetActions.deletingRemovesItFromNew")}</p>
      <button type="button" className="act" data-action="agent-preset.delete" disabled={busy} onClick={() => void run(async () => {
        await port.deleteAgentPreset(d.id, version.revision);
        onDeleted();
      })}>{t("common.confirmDelete")}</button>
      <button type="button" className="act" data-action="agent-preset.delete-cancel" disabled={busy} onClick={() => setDeleting(false)}>{t("presetActions.cancelDeletion")}</button>
    </div>}
    <details>
      <summary>{t("presetActions.explicitDefaultConfiguration")}</summary>
      <p className="ds">{t("presetActions.usedOnlyWhenExplicitlySelecting")}</p>
      <div className="composition-toolbar">
        <button type="button" className="act" data-action="agent-preset.default" disabled={busy || d.id.startsWith("project:")}
          onClick={() => void run(async () => { await port.setDefaultAgentPreset(d.id, false); setNote(t("presetActions.setAsPersonalPreference")); })}>{t("presetActions.setPersonalPreference")}</button>
        <button type="button" className="act" data-action="agent-preset.workspace-default" disabled={busy}
          onClick={() => void run(async () => { await port.setDefaultAgentPreset(d.id, true); setNote(t("presetActions.setAsWorkspacePreference")); })}>{t("presetActions.setWorkspacePreference")}</button>
      </div>
      {note && <p role="status">{note}</p>}
    </details>
  </details>;
}
