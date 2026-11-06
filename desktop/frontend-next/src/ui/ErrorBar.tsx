import { t } from "../i18n";
import { StudioIcon } from "./StudioIcon";
import { reportProblem } from "./reportproblem";

/** A refused action's reason, kept in the window's corner until it is dismissed.
 *  With onReport it can be sent as a report from where it appeared. */
export function ErrorBar({ text, onDismiss, onReport }: { text: string; onDismiss: () => void; onReport?: () => void }) {
  return (
    <div className="errbar" role="alert">
      <StudioIcon name="fail" />
      <span>{text}</span>
      {onReport && <button data-action="errbar.report" onClick={() => { reportProblem(text); onReport(); onDismiss(); }}>{t("app.report_this_problem")}</button>}
      <button onClick={onDismiss}>{t("common.got_it")}</button>
    </div>
  );
}
