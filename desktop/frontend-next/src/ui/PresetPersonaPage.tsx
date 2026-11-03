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
        <button type="button" className="preset-back" data-action="agent-preset.leave" onClick={() => leave.request(onClose)}><StudioIcon name="chevron" />{t("common.backToPresets")}</button>
        <h2>{t("presetPersonaPage.everydayPartnerPersona")}</h2>
        <p>{t("presetPersonaPage.editUserConfigurationSavedSeparately")}</p>
      </header>
      <div className="preset-page-content"><Persona port={port} onDirtyChange={setDirty} onBusyChange={setBusy} onSaved={onClose} /></div>
      <footer className="preset-page-footer"><p>{t("presetPersonaPage.appliesToNewConversationsAnd")}</p>
        <button type="button" className="btn" data-action="agent-preset.leave" onClick={() => leave.request(onClose)}>{t("common.back")}</button></footer>
    </div>
    {leave.confirmation}
  </>;
}
