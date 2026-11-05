import { useEffect, useState } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { PillButton } from "./SettingRow";
import { SectionCard } from "./SectionCard";

export function ChangedSettings({ port }: { port: AgentPort }) {
  const [keys, setKeys] = useState<string[] | null>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    let live = true;
    void port.changedSettings().then((read) => live && setKeys(read.keys)).catch(() => live && setKeys(null));
    return () => { live = false; };
  }, [port]);
  if (keys === null) return null;
  return (
    <SectionCard title={t("settings.changed_settings")}>
      <div className="srow">
        <div className="srow-meta">
          <span className="srow-name">{keys.length === 0 ? t("settings.every_setting_is_the_default") : t("settings.n_differ_from_the_defaults", { n: keys.length })}</span>
        </div>
        {keys.length > 0 && (
          <div className="srow-ctl">
            <PillButton action="settings.changed-toggle" onClick={() => setOpen((v) => !v)}>{t(open ? "common.collapse" : "settings.look_at")}</PillButton>
          </div>
        )}
      </div>
      {open && <ul className="changed-keys">{keys.map((key) => <li key={key}>{key}</li>)}</ul>}
    </SectionCard>
  );
}
