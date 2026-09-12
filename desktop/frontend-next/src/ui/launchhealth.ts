import { useEffect, useRef } from "react";
import type { AgentPort, ProviderSetup } from "../port/port";

// How long a launch has to stay up before it may retire the update it booted
// from. The desktop this replaced waited the same two seconds for the same
// reason: a build that comes up and dies immediately must not be the one that
// throws away the way back to the build before it.
const PROBATION_MS = 2000;

/** Says this launch works, once, after it has stayed up long enough to mean it.
 *
 *  That sentence is what retires the update the launch booted from. The swap is
 *  performed by a process that cannot judge the result, so the rollback
 *  material is kept until the application that came up reports in — and nothing
 *  in this tree reported. The endpoint existed with no caller, so the
 *  transaction was never closed and every install after the first refused: a
 *  pending update already exists.
 *
 *  Said from the page rather than the shell because a window that opened and
 *  then failed to render is exactly the case the rollback exists for, and only
 *  the page knows it got past that. Silent either way: nothing here was asked
 *  for by anyone, and a launch that booted from no update has nothing to retire.
 */
export function useLaunchHealth(port: AgentPort | null, setup: ProviderSetup | null | undefined, welcomed: boolean | undefined) {
  const said = useRef(false);
  useEffect(() => {
    if (said.current || !port) return;
    // Onboarding is not "up": an application still asking for a key is one the
    // user cannot use yet, and retiring the way back from inside it discards
    // the rollback on a launch that has proven nothing.
    if (setup === undefined || welcomed === undefined || !welcomed || setup?.required) return;
    const timer = setTimeout(() => {
      said.current = true;
      void port.acknowledgeLaunchHealth();
    }, PROBATION_MS);
    return () => clearTimeout(timer);
  }, [port, setup, welcomed]);
}
