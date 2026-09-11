import { useState, type ComponentProps } from "react";
import type { WorkspacePanel } from "../components/WorkspacePanel";

const requestKeys = ["revealPathRequest", "changeRevealRequest", "verificationRevealRequest", "fileListRequest", "changeListRequest"] as const;
type Requests = Pick<ComponentProps<typeof WorkspacePanel>, typeof requestKeys[number]>;
const empty: Requests = Object.fromEntries(requestKeys.map(key => [key, null]));

// A reveal belongs to the visible view that received it. Retained command props
// must not replay into another view or overwrite navigation when one remounts.
export function useDockViewRequests(scope: string, view: string | null, incoming: Requests): Requests {
  const source = Object.fromEntries(requestKeys.map(key => [key, incoming[key]])) as Requests;
  const [state, setState] = useState(() => ({ scope, view, source: view ? source : empty, forwarded: view ? source : empty }));
  const sameView = state.scope === scope && state.view === view;
  if (!sameView || (view && requestKeys.some(key => state.source[key] !== incoming[key]))) {
    const forwarded = view ? Object.fromEntries(requestKeys.map(key => [
      key, state.source[key] !== incoming[key] ? incoming[key] : sameView ? state.forwarded[key] : null,
    ])) as Requests : empty;
    const next = { scope, view, source: view ? source : state.source, forwarded };
    setState(next);
    return forwarded;
  }
  return state.forwarded;
}
