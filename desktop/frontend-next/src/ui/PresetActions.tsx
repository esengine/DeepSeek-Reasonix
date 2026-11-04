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
    <summary>{t("common.more_actions")}</summary>
    {d.id === "builtin:general" && <button type="button" className="act" data-action="agent-preset.persona" disabled={busy} onClick={onPersona}>{t("persona.edit_everyday_partner_persona")}</button>}
    <div className="composition-toolbar">
      <button type="button" className="act" data-action="agent-preset.export" disabled={busy} onClick={() => void run(async () => {
        setExported(JSON.stringify(await port.exportAgentPreset(d.id), null, 2));
      })}>{t("persona.export_file")}</button>
      {!d.id.startsWith("builtin:") && <button type="button" className="act" data-action="agent-preset.delete-preview" disabled={busy} onClick={() => setDeleting(true)}>{t("persona.delete_preset")}</button>}
    </div>
    {exported && <a className="act" download="agent-preset.json"
      href={`data:application/json;charset=utf-8,${encodeURIComponent(exported)}`}>{t("persona.download_preset_json")}</a>}
    {deleting && <div className="find" role="group" aria-label={t("persona.delete_preset")}>
      <p>{t("persona.deleting_removes_it_from_new")}</p>
      <button type="button" className="act" data-action="agent-preset.delete" disabled={busy} onClick={() => void run(async () => {
        await port.deleteAgentPreset(d.id, version.revision);
        onDeleted();
      })}>{t("common.confirm_delete")}</button>
      <button type="button" className="act" data-action="agent-preset.delete-cancel" disabled={busy} onClick={() => setDeleting(false)}>{t("persona.cancel_deletion")}</button>
    </div>}
    <details>
      <summary>{t("persona.explicit_default_configuration")}</summary>
      <p className="ds">{t("persona.used_only_when_explicitly_selecting")}</p>
      <div className="composition-toolbar">
        <button type="button" className="act" data-action="agent-preset.default" disabled={busy || d.id.startsWith("project:")}
          onClick={() => void run(async () => { await port.setDefaultAgentPreset(d.id, false); setNote(t("persona.set_as_personal_preference")); })}>{t("persona.set_personal_preference")}</button>
        <button type="button" className="act" data-action="agent-preset.workspace-default" disabled={busy}
          onClick={() => void run(async () => { await port.setDefaultAgentPreset(d.id, true); setNote(t("persona.set_as_workspace_preference")); })}>{t("persona.set_workspace_preference")}</button>
      </div>
      {note && <p role="status">{note}</p>}
    </details>
  </details>;
}
