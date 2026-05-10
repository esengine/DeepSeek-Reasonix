import { useCallback, useState } from "react";
import { type ResolvedHook, loadHooks } from "../../../hooks.js";

export interface HookList {
  hookList: ResolvedHook[];
  /** `loadHooks(projectRoot)` + state replacement — returns the fresh count for the slash handler's reply. */
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
