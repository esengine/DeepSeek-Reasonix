import { t } from "../../i18n";
import { StudioIcon } from "../StudioIcon";
import type { SkillPin } from "./skillpin";

/** A skill invocation as a message: the skill's name, what the person typed after
 *  it, and the skill's own text folded away until asked for. */
export function SkillPinView({ pin }: { pin: SkillPin }) {
  return (
    <details className="skillpin">
      <summary>
        <StudioIcon name="spark" />
        <b>{pin.name}</b>
        {pin.args && <span className="skillpin-args">{pin.args}</span>}
        <span className="skillpin-more">{t("chat.skill_text")}</span>
      </summary>
      {pin.description && <p className="skillpin-desc">{pin.description}</p>}
      <pre className="skillpin-text">{pin.text}</pre>
    </details>
  );
}
