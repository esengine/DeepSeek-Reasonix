import { useState } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { Persona } from "./Persona";
import { StudioIcon } from "./StudioIcon";
import { usePresetLeave } from "./PresetLeave";

export function PresetPersonaPage({ port, onClose }: { port: AgentPort; onClose: () => void }) {
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const leave = usePresetLeave(dirty, busy);
  return <>
    <div className="preset-page" data-action-keydown="agent-preset.leave" onKeyDown={(e) => {
      if (e.key !== "Escape") return;
      e.preventDefault(); e.stopPropagation(); leave.request(onClose);
    }}>
      <header className="preset-page-heading">
        <button type="button" className="preset-back" data-action="agent-preset.leave" onClick={() => leave.request(onClose)}><StudioIcon name="chevron" />{t("persona.back_to_presets")}</button>
        <h2>{t("persona.everyday_partner_persona")}</h2>
        <p>{t("persona.edit_user_configuration_saved_separately")}</p>
      </header>
      <div className="preset-page-content"><Persona port={port} onDirtyChange={setDirty} onBusyChange={setBusy} onSaved={onClose} /></div>
      <footer className="preset-page-footer"><p>{t("persona.applies_to_new_conversations_and")}</p>
        <button type="button" className="btn" data-action="agent-preset.leave" onClick={() => leave.request(onClose)}>{t("common.back")}</button></footer>
    </div>
    {leave.confirmation}
  </>;
}
