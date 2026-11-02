import { useMemo } from "react";
import { SETTINGS, settingMatches } from "./prefsnav";
import type { Section } from "./prefsnav";
import { useShellGraphics } from "./GraphicsSection";
import { host } from "../port/host";

/** The settings a query finds. `shown` decides which sections can be shown and
 *  `shownKey` is what changes its answer; the graphics entry exists only where
 *  the shell answers for graphics. */
export function useSettingsSearch(query: string, shown: (id: Section) => boolean, shownKey: unknown) {
  const shell = useShellGraphics();
  const localShell = host().inShell();
  return useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    return SETTINGS.filter((e) => !e.hidden && shown(e.section) && (e.anchor !== "graphics" || shell !== null) && (e.anchor !== "global-shortcut" || localShell) && settingMatches(e, q));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, shownKey, shell, localShell]);
}
