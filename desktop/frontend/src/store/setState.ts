import type { SetStateAction } from "react";

// applySetState mirrors React's setState contract: resolve the next value from
// either a direct value or an updater (prev => next). It lets a zustand store
// expose Dispatch<SetStateAction<T>> setters that are drop-in replacements for
// useState — so migrated call sites, including functional updaters like
// setOpen(v => !v), need no changes.
export function applySetState<T>(prev: T, update: SetStateAction<T>): T {
  return typeof update === "function" ? (update as (value: T) => T)(prev) : update;
}
