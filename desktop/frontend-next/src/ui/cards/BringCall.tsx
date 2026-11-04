import { t } from "../../i18n";
import { StudioIcon } from "../StudioIcon";

/** Shows this call's own result again at the end of the transcript, so a reader
 *  does not have to find the card the model summarised. */
export function BringCall({ onBring }: { onBring: () => void }) {
  return (
    <button type="button" className="bring-call" data-action="tool.bring" aria-label={t("chat.show_this_result_again_at")} title={t("chat.show_this_result_again_at")} onClick={(event) => {
        // It sits in the call's heading line, which is also what folds the card.
        event.preventDefault();
        event.stopPropagation();
        onBring();
      }}>
      <StudioIcon name="arrow" />
    </button>
  );
}
