import { useCallback, useState } from "react";
import { type ResolvedHook, loadHooks } from "../../../hooks.js";

export interface HookList {
  hookList: ResolvedHook[];
  /** Reload hooks from disk and return the active count for the slash handler. */
  reloadHooks: (projectRoot: string | undefined) => number;
}

export function useHookList(initialProjectRoot: string | undefined): HookList {
  const [hookList, setHookList] = useState<ResolvedHook[]>(() =>
    loadHooks({ projectRoot: initialProjectRoot }),
  );
  const reloadHooks = useCallback((projectRoot: string | undefined): number => {
    const fresh = loadHooks({ projectRoot });
    setHookList(fresh);
    return fresh.length;
  }, []);
  return { hookList, reloadHooks };
}
