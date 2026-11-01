import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AgentPort } from "../../port/port";
import type { PreviewInfo } from "../../port/workspace";
import { parseGrantedFile } from "../grantedfiles";
import { reason } from "../../i18n/kernel";
import { previewsOffice } from "../../state/prefs";

type Loaded = { info: PreviewInfo; data?: ArrayBuffer; error?: string };

export function useDocument(port: AgentPort, path: string, shown: boolean, refresh: string) {
  const held = useMemo(() => parseGrantedFile(path), [path]);
  const ref = useMemo(() => ({ path: held?.rel ?? path, ...(held ? { root: held.root } : {}) }), [held, path]);
  const [state, setState] = useState<{ ref: typeof ref; port: AgentPort; value: Loaded }>();
  const [reload, setReload] = useState(0);
  const cache = useRef<Loaded | undefined>(undefined);
  const lastReload = useRef(0);
  const request = useCallback(() => setReload((n) => n + 1), []);
  useEffect(() => {
    cache.current = undefined;
    setState(undefined);
  }, [ref, port]);
  useEffect(() => {
    if (!shown) return;
    const controller = new AbortController();
    let busy = false;
    const read = async (force: boolean) => {
      if (busy || controller.signal.aborted) return;
      busy = true;
      let observed: PreviewInfo | undefined;
      try {
        const info = await port.workspacePreviewInfo(ref, controller.signal);
        observed = info;
        if (controller.signal.aborted) return;
        let value: Loaded;
        if (info.kind === "unsupported" || info.size > info.limit || (info.kind === "office" && !previewsOffice())) value = { info };
        else if (!force && cache.current?.data && cache.current.info.version === info.version) value = { ...cache.current, info, error: undefined };
        else {
          const loaded = await port.workspacePreviewContent(ref, controller.signal);
          value = { info: { ...info, version: loaded.version || info.version }, data: loaded.data };
        }
        if (!controller.signal.aborted) { cache.current = value; setState({ ref, port, value }); }
      } catch (e) {
        if (!controller.signal.aborted) {
          cache.current = undefined;
          setState({ ref, port, value: { info: observed ?? { ref, kind: "unsupported", size: 0, limit: 0, version: "", readOnly: true }, error: reason(e) } });
        }
      } finally { busy = false; }
    };
    const force = lastReload.current !== reload;
    lastReload.current = reload;
    void read(force);
    const timer = window.setInterval(() => void read(false), 3000);
    return () => { controller.abort(); window.clearInterval(timer); };
  }, [port, ref, shown, refresh, reload]);
  return { value: state?.ref === ref && state.port === port ? state.value : undefined, reload: request };
}
