import { useEffect, useState } from "react";
import { PanelBottom } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { applySessionExperience, getSessionExperience, type SessionExperience } from "../lib/sessionExperience";
import { hydrateReasoningDisplayMode } from "../lib/reasoningDisplayPreference";
import type { SettingsView } from "../lib/types";
import { SettingsField, SettingsSection } from "./SettingsForm";

type Props = {
  snapshot: SettingsView;
  busy: boolean;
  apply: (write: () => Promise<unknown>) => Promise<boolean>;
};

export function SessionExperienceSettings({ snapshot, busy, apply }: Props) {
  const t = useT();
  const [mode, setMode] = useState<SessionExperience>(getSessionExperience);
  const present = useCommittedCommand((next: SessionExperience) => {
    setMode(next);
    applySessionExperience(next);
    hydrateReasoningDisplayMode(next === "deep" ? "expanded" : "auto", next === "deep");
  });
  // Snapshot identity matters: a failed write may reload the same backend value.
  useEffect(() => { present(snapshot.sessionExperience === "deep" ? "deep" : "standard"); }, [snapshot, present]);
  const save = useCommittedCommand(async (next: SessionExperience) => {
    present(next);
    // The shared Settings apply/reload path owns both success and failure.
    await apply(() => app.SetSessionExperience(next));
  });
  return <SettingsSection title={t("settings.general.sectionConversation")} description={t("settings.sessionExperienceHint")}>
    <SettingsField label={t("settings.sessionExperience")} hint={mode === "deep" ? t("settings.sessionExperience.deepHint") : t("settings.sessionExperience.standardHint")} icon={<PanelBottom size={18} />}>
      <div className="set-seg" role="radiogroup" aria-label={t("settings.sessionExperience")}>
        {(["standard", "deep"] as const).map(value => <button key={value} type="button"
          className={`set-seg__btn${mode === value ? " set-seg__btn--on" : ""}`} role="radio"
          aria-checked={mode === value} disabled={busy} onClick={() => void save(value)}>
          {t(`settings.sessionExperience.${value}`)}
        </button>)}
      </div>
    </SettingsField>
  </SettingsSection>;
}
