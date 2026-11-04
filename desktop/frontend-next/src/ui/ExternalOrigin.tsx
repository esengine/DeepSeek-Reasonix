import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason, savedNotApplied } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import { SaveNote, type SaveNoteState } from "./SaveNote";
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
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("safety.ask_about_text_from_outside")}</span>
          <span className="ds">{t("safety.when_a_call_copies_text")}</span>
        </span>
        <Switch data-action="permissions.ask-on-external-origin" on={on} busy={busy} label={t("safety.ask_about_text_from_outside")} onClick={() => void flip()} />
      </div>
      <SaveNote note={note} />
    </div>
  );
}
