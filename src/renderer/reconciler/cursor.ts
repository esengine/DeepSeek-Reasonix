/** Cursor request from React → renderer. mount() polls this on every frame commit. */

import { createContext, useContext, useEffect } from "react";

export interface CursorTarget {
  /** Column from the left edge of the viewport (0-based). Wide chars count as 2. */
  readonly col: number;
  /** Row from the BOTTOM of the rendered screen (0 = last row). Default 0. */
  readonly rowFromBottom?: number;
  /** Hide the terminal cursor when false. Default true. */
  readonly visible?: boolean;
}

export type CursorSetter = (target: CursorTarget | null) => void;

export const CursorContext = createContext<CursorSetter | null>(null);

/** Register / replace / clear the cursor target. Pass `null` to release. */
export function useCursor(target: CursorTarget | null): void {
  const setter = useContext(CursorContext);
  // Snapshot to primitive scalars so re-renders with structurally equal
  // targets don't re-fire the effect (callers usually pass fresh object literals).
  const isSet = target !== null;
  const col = target?.col ?? 0;
  const rowFromBottom = target?.rowFromBottom ?? 0;
  const visible = target?.visible !== false;
  useEffect(() => {
    if (!setter) return;
    if (!isSet) {
      setter(null);
      return;
    }
    setter({ col, rowFromBottom, visible });
    return () => setter(null);
  }, [setter, isSet, col, rowFromBottom, visible]);
}
