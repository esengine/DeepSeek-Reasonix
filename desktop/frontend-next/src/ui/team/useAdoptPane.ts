import { type Dispatch, type SetStateAction, useCallback, useEffect } from "react";
import type { RuntimeView } from "../../port/hub";
import type { SessionSwitch } from "../sessionswitch";
import { closeGroupStage, useGroupStage } from "../../state/teamStage";

// A pane the team opened in the kernel, brought into the window: listed once
// and focused, the way a pane the window opened itself would be.
export function useAdoptPane(setRuntimes: Dispatch<SetStateAction<RuntimeView[]>>, focusPane: (id: string) => void, sw: SessionSwitch) {
  const group = useGroupStage();
  useEffect(() => { if (group) sw.cancel(); }, [group, sw.cancel]);
  return useCallback(
    async (pending: RuntimeView | Promise<RuntimeView>) => {
      const ticket = sw.begin("");
      try {
        const rt = await pending;
        setRuntimes((cur) => (cur.some((p) => p.id === rt.id) ? cur : [...cur, rt]));
        if (sw.current(ticket)) {
          closeGroupStage();
          focusPane(rt.id);
        }
      } catch (error) {
        if (sw.current(ticket)) throw error;
      } finally {
        sw.end(ticket);
      }
    },
    [setRuntimes, focusPane, sw.begin, sw.current, sw.end],
  );
}
