import { useId } from "react";
import { t } from "../i18n";
import { boardNameProblem } from "./boardname";

interface Props {
  checked: boolean;
  name: string;
  disabled?: boolean;
  onChecked: (on: boolean) => void;
  onName: (name: string) => void;
}

const WORDS = {
  short: "feedback.the_name_needs_at_least",
  long: "feedback.the_name_can_have_at",
  chars: "feedback.the_name_may_use_letters",
} as const;

// The choice that rides with linking: a public name on the contribution board,
// offered already ticked and applied only when the person confirms.
export function FeedbackBoardJoin({ checked, name, disabled, onChecked, onName }: Props) {
  const id = useId();
  const typed = name.trim();
  const problem = checked && typed ? boardNameProblem(typed) : null;
  return (
    <div className="fbk-board-join">
      <label className="fbk-board-join-pick">
        <input type="checkbox" data-action-change="feedback.board-join-choice" checked={checked} disabled={disabled} onChange={(e) => onChecked(e.target.checked)} />
        <span>{t("feedback.join_the_board")}</span>
      </label>
      <p className="fbk-hint">{t("feedback.the_board_is_public_anyone")}</p>
      {checked && (
        <>
          <input
            id={id}
            className="fbk-board-name-field"
            data-action-change="feedback.board-name"
            aria-label={t("feedback.name_shown_on_the_board")}
            aria-invalid={problem ? true : undefined}
            value={name}
            maxLength={40}
            disabled={disabled}
            placeholder={t("feedback.name_shown_on_the_board")}
            onChange={(e) => onName(e.target.value)}
          />
          {problem ? <p className="fbk-hint" role="alert" data-tone="error">{t(WORDS[problem])}</p>
            : typed ? <p className="fbk-hint" aria-live="polite">{t("feedback.it_will_be_shown_on", { name: typed })}</p>
            : <p className="fbk-hint">{t("feedback.enter_a_name_or_untick")}</p>}
        </>
      )}
    </div>
  );
}
