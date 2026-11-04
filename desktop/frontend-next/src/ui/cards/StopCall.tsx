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

/** The line under a running step in the activity group, on the same clock as
 *  StopCall: what it has produced so far, and a way to stop it alone. */
export function StepWait({ onStop, quiet }: { onStop?: () => void; quiet: boolean }) {
  const watched = useWatched(true);
  if (watched < AFTER_MS || (quiet && !onStop)) return null;
  return (
    <div className="step-wait">
      {!quiet && t("panels.no_output_yet")}
      {!quiet && onStop && " · "}
      {onStop && <button type="button" className="step-stop" data-action="tool.stop" onClick={onStop}>{t("chat.stop_this_one")}</button>}
    </div>
  );
}
