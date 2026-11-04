import { useCallback, useEffect, useRef } from "react";
import type { Dispatch } from "react";
import type { AgentPort } from "../port/port";
import type { ExtensionSurface, WireEvent } from "../port/wire";
import type { SessionEvent } from "../state/session";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

const key = (p: ExtensionSurface) => JSON.stringify([p.pluginId, p.surfaceId]);

export function useSurfaceReplay(port: AgentPort, dispatch: Dispatch<SessionEvent>) {
  const reads = useRef(new Set<Set<string>>());
  const epoch = useRef(0);
  const request = useRef(0);
  const observe = useCallback((ev: WireEvent) => {
    if (ev.extension) {
      for (const changed of reads.current) changed.add(key(ev.extension));
    }
  }, []);
  const refresh = useCallback(async (current: () => boolean = () => true) => {
    const generation = epoch.current;
    const id = ++request.current;
    const changed = new Set<string>();
    reads.current.add(changed);
    try {
      const { surfaces, incomplete } = await port.extensionSurfaces();
      if (generation !== epoch.current || id !== request.current || !current()) return;
      dispatch({ kind: "__extension_snapshot", surfaces: surfaces.filter(p => !changed.has(key(p))) });
      if (incomplete) dispatch({ kind: "__error", text: t("chat.some_extension_surfaces_may_not") });
    } catch (error) {
      if (generation === epoch.current && id === request.current && current()) {
        dispatch({ kind: "__error", text: reason(error) });
      }
    } finally {
      reads.current.delete(changed);
    }
  }, [port, dispatch]);
  useEffect(() => {
    void refresh();
    return () => { epoch.current++; reads.current.clear(); };
  }, [refresh]);
  return { refresh, observe };
}
