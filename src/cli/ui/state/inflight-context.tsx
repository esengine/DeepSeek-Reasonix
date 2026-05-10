// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { createContext, useContext, useSyncExternalStore } from "react";
import type { InflightSet } from "../../../core/inflight.js";

const Ctx = createContext<InflightSet | null>(null);

export function InflightProvider({
  inflight,
  children,
}: {
  inflight: InflightSet;
  children: React.ReactNode;
}): React.ReactElement {
  return <Ctx.Provider value={inflight}>{children}</Ctx.Provider>;
}

/** True iff the loop currently has `id` in its inflight set. Re-renders on every set mutation; React bails on unchanged boolean snapshot. */
export function useIsInflight(id: string): boolean {
  const inflight = useContext(Ctx);
  return useSyncExternalStore(
    (cb) => (inflight ? inflight.subscribe(cb) : noop),
    () => (inflight ? inflight.has(id) : false),
    () => false,
  );
}

const noop = () => {};
