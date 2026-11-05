import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason, savedNotApplied } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { SaveNote, type SaveNoteState } from "./SaveNote";
import { SettingRow } from "./SettingRow";
import { Switch } from "./Switch";

// One switch over asking before a call that copies text an external result
// gave the task. The save sends the lists as the kernel holds them now, because
// the rules editor saves the same record and a stale copy would overwrite it.
export function ExternalOrigin({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [on, setOn] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<SaveNoteState | null>(null);

  useEffect(() => {
    port.permissions().then((p) => setOn(p.askOnExternalOrigin === true)).catch(() => setOn(null));
  }, [port]);

  if (on === null) return <div className="empty">{t("safety.this_setting_could_not_be")}</div>;

  const flip = async () => {
    setBusy(true);
    setNote(null);
    try {
      const { mode, deny, ask, allow } = await port.permissions();
      const saved = await port.savePermissions({ mode, deny, ask, allow, askOnExternalOrigin: !on });
      setOn(saved.askOnExternalOrigin === true);
      onChanged();
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <SettingRow id="external-origin" title={t("settings.outside_text")} desc={t("safety.ask_before_copied_text")} more={t("safety.what_outside_text_covers")}>
        <Switch data-action="permissions.ask-on-external-origin" on={on} busy={busy} label={t("settings.outside_text")} onClick={() => void flip()} />
      </SettingRow>
      <SaveNote note={note} />
    </>
  );
}
