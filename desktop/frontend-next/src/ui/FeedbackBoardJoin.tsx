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
  short: "feedbackBoardJoin.theNameNeedsAtLeast",
  long: "feedbackBoardJoin.theNameCanHaveAt",
  chars: "feedbackBoardJoin.theNameMayUseLetters",
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
        <span>{t("common.joinTheBoard")}</span>
      </label>
      <p className="fbk-hint">{t("feedbackBoardJoin.theBoardIsPublicAnyone")}</p>
      {checked && (
        <>
          <input
            id={id}
            className="fbk-board-name-field"
            data-action-change="feedback.board-name"
            aria-label={t("common.nameShownOnTheBoard")}
            aria-invalid={problem ? true : undefined}
            value={name}
            maxLength={40}
            disabled={disabled}
            placeholder={t("common.nameShownOnTheBoard")}
            onChange={(e) => onName(e.target.value)}
          />
          {problem ? <p className="fbk-hint" role="alert" data-tone="error">{t(WORDS[problem])}</p>
            : typed ? <p className="fbk-hint" aria-live="polite">{t("common.itWillBeShownOn", { name: typed })}</p>
            : <p className="fbk-hint">{t("feedbackBoardJoin.enterANameOrUntick")}</p>}
        </>
      )}
    </div>
  );
}
