import { useMemo } from "react";
import { SETTINGS } from "./prefsnav";
import { pageIndexOf } from "./settingPageIndex";
import { searchSettings } from "./settingSearch";
import type { Section } from "./prefsnav";
import { useShellGraphics } from "./GraphicsSection";
import { host } from "../port/host";

/** The settings this machine can show. `shown` decides which sections can be
 *  shown and `shownKey` is what changes its answer; the graphics entry exists
 *  only where the shell answers for graphics. */
function useVisibleSettings(shown: (id: Section) => boolean, shownKey: unknown) {
  const shell = useShellGraphics();
  const localShell = host().inShell();
  return useMemo(
    () => SETTINGS.filter((e) => !e.hidden && shown(e.section) && (e.anchor !== "graphics" || shell !== null) && (e.anchor !== "global-shortcut" || localShell)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shownKey, shell, localShell],
  );
}

/** The settings a query finds, best first. */
export function useSettingsSearch(query: string, shown: (id: Section) => boolean, shownKey: unknown) {
  const visible = useVisibleSettings(shown, shownKey);
  return useMemo(() => {
    const q = query.trim();
    return q ? searchSettings(visible, q) : null;
  }, [query, visible]);
}

/** What a page holds, for its index. */
export function usePageIndex(at: Section, shown: (id: Section) => boolean, shownKey: unknown) {
  const visible = useVisibleSettings(shown, shownKey);
  return useMemo(() => pageIndexOf(visible, at), [visible, at]);
}
