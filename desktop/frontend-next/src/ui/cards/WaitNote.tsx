import { t } from "../../i18n";
import { seconds } from "../../i18n/format";
import { useWatched } from "../Cost";

// The only timeout the page can state is the one the call carries itself; the
// host's own default is configuration this window does not hold.
function ownTimeout(args: string | undefined): number {
  try {
    const n = Number((JSON.parse(args ?? "{}") as { timeout_seconds?: unknown }).timeout_seconds);
    return Number.isFinite(n) && n > 0 ? n : 0;
  } catch {
    return 0;
  }
}

/** What a running call with no output yet says about itself, so a card that sits
 *  still reads as "still running, nothing printed" rather than as stuck. */
export function WaitNote({ args }: { args?: string }) {
  const watched = useWatched(true);
  const limit = ownTimeout(args);
  return (
    <div className="activity-call-wait" role="status">
      {t("chat.no_output_yet_the_command")}
      {watched >= 1000 && <> · {t("chat.running_for_time", { time: seconds(watched, 0) })}</>}
      {limit > 0 && <> · {t("chat.this_commands_own_timeout_is", { n: limit })}</>}
    </div>
  );
}
