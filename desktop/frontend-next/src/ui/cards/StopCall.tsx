import { t } from "../../i18n";
import { useWatched } from "../Cost";

const AFTER_MS = 10_000;

/** One call, stopped alone. It appears only once a call has run long enough to
 *  look stuck, so a quick call never offers it. */
export function StopCall({ onStop }: { onStop: () => void }) {
  const watched = useWatched(true);
  if (watched < AFTER_MS) return null;
  return (
    <button type="button" className="stop-call" data-action="tool.stop" onClick={onStop}>
      {t("chat.stop_only_this_call")}
    </button>
  );
}
