import { useCallback, useEffect, useMemo, useState } from "react";
import type { AgentPort } from "../port/port";
import type { SessionState } from "../state/session_types";
import { placement } from "./slots";

type Views = SessionState["views"];

/** Where the user put each extension surface, and the split of the pane's views
 *  into those beside the composer and those in the rail. */
export function useSurfaceSlots(port: AgentPort, views: Views, fail: (e: unknown) => void) {
  const [slots, setSlots] = useState<Record<string, string>>({});

  useEffect(() => {
    void port.surfaceSlots().then(setSlots).catch(() => setSlots({}));
  }, [port]);

  // Updated in place: a move is the user's own action, so waiting for a round
  // trip would read as a dead click.
  const moveSurface = useCallback(
    async (ext: { pluginId: string; surfaceId: string }, slot: string) => {
      const id = `${ext.pluginId}:${ext.surfaceId}`;
      setSlots((prev) => {
        const next = { ...prev };
        if (slot) next[id] = slot;
        else delete next[id];
        return next;
      });
      await port.assignSurface(id, slot).catch(fail);
    },
    [port, fail],
  );

  // Split once per change rather than per frame: a new array each render is a
  // changed prop, and that alone would keep the rail re-rendering all turn.
  const [atComposer, inRail] = useMemo(() => {
    const at: Views = [];
    const rail: Views = [];
    for (const v of views) (placement(v, slots) === "composer-trailing" ? at : rail).push(v);
    return [at, rail];
  }, [views, slots]);

  return { slots, moveSurface, atComposer, inRail };
}
