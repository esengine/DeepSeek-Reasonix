import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SkillEntry } from "../port/port";
import { HttpError } from "../port/http_error";

const CHANGED = "skill.changed";

// What removing a skill would delete, shown before the person agrees. The
// request carries the scope and path drawn here, so a skill that changed under
// the same name since the list was read is refused, not removed.
export function SkillRemove({ port, sk, onClose, onDone }: { port: AgentPort; sk: SkillEntry; onClose: () => void; onDone: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [stale, setStale] = useState(false);

  const remove = async () => {
    setBusy(true);
    setError("");
    try {
      await port.removeSkill(sk.name, sk.scope ?? "", sk.path ?? "", undefined);
      onDone();
    } catch (e) {
      setStale(e instanceof HttpError && e.reason?.code === CHANGED);
      setError(reason(e));
      setBusy(false);
    }
  };

  return (
    <div className="skupd mkt addpkg" data-stage="confirm" aria-busy={busy}>
      <div className="find" data-lvl="warn">
        <span className="t">{t("ext.remove_the_skill_name", { name: sk.name })}</span>
        <span className="why">{t(sk.removesFolder ? "ext.this_deletes_the_folder_below" : "ext.this_deletes_the_file_below")}</span>
        <code>{sk.removes}</code>
      </div>
      <div className="acts">
        {stale && <button className="act" data-action="skills.remove-reload" onClick={onDone}>{t("common.read_again")}</button>}
        <button className="act" data-action="skills.remove-cancel" disabled={busy} onClick={onClose}>{t("common.cancel")}</button>
        <button className="act danger" data-action="skills.remove-confirm" disabled={busy} onClick={() => void remove()}>{t(busy ? "ext.removing" : "ext.remove")}</button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
