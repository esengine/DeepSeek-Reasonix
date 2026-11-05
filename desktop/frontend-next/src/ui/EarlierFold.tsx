import { useMemo, useState, type ReactNode } from "react";
import { t } from "../i18n";
import type { Item } from "../state/session";
import { compactionFold } from "./compactionfold";
import { StudioIcon } from "./StudioIcon";

/** What the transcript shows once earlier compactions fold what they replaced,
 *  and the divider line that opens and closes that fold. */
export function useEarlierFold(every: Item[]): { items: Item[]; bar: ReactNode } {
  const [unfolded, setUnfolded] = useState(false);
  const earlier = useMemo(() => compactionFold(every), [every]);
  const items = useMemo(() => (unfolded ? every : every.slice(earlier.cut)), [every, earlier.cut, unfolded]);
  const bar = earlier.cut > 0 && (
    <button type="button" className="fold-earlier" data-action="transcript.unfold-compacted" aria-expanded={unfolded} onClick={() => setUnfolded((on) => !on)}>
      <i aria-hidden="true" />
      <StudioIcon name="down" />
      <span>{t("chat.n_earlier_messages_folded", { n: earlier.folded })}</span>
      <b>{t(unfolded ? "chat.hide_the_folded_content" : "chat.show_the_folded_content")}</b>
      <i aria-hidden="true" />
    </button>
  );
  return { items, bar };
}
