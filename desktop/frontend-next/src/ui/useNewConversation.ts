import { useCallback } from "react";
import type { RuntimeView } from "../port/hub";

export function useNewConversation(active: string, runtimes: RuntimeView[], fallback: string | undefined,
  open: (req: { root?: string }) => Promise<void>,
  openRemote: (host: string, root?: string) => Promise<void>) {
  return useCallback(() => {
    const runtime = runtimes.find((rt) => rt.id === active);
    return runtime?.host ? openRemote(runtime.host, runtime.root) : open({ root: runtime?.root ?? fallback });
  }, [active, runtimes, fallback, open, openRemote]);
}
