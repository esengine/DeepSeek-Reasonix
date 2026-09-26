import { useEffect, useState } from "react";
import type { ContextPanelInfo } from "./types";

// The snapshot is refetched only on usage events, so a turn running tools
// between model requests keeps counting here instead of in the host.
export function useSessionRuntimeMs(info: Pick<ContextPanelInfo, "elapsedMs" | "activeTurnStartedAt"> | null | undefined): number {
  const startedAt = info?.activeTurnStartedAt ?? 0;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (startedAt <= 0) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [startedAt]);
  const completed = Math.max(0, info?.elapsedMs ?? 0);
  return startedAt > 0 ? completed + Math.max(0, now - startedAt) : completed;
}
