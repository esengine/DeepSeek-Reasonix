import { useCallback, useEffect, useState } from "react";
import type { AgentPort } from "../port/port";
import type { AgentRequest } from "./useAgentTabs";

/** Opening a task card's sub-agents in the workbench. The kernel's list pairs a
 *  run with the tool call that spawned it, so the card names its runs exactly.
 *  False means this call has no run to read — an older session, or a kernel
 *  without the route — and the caller keeps its own view. */
export function useAgentRequest(port: AgentPort, reveal: () => void) {
  const [request, setRequest] = useState<AgentRequest>();
  useEffect(() => () => setRequest(undefined), [port]);
  const open = useCallback(async (call: string): Promise<boolean> => {
    let runs;
    try {
      runs = (await port.subagents()).filter((run) => run.parentCall === call);
    } catch {
      return false;
    }
    if (runs.length === 0) return false;
    const req: AgentRequest = {
      runs: runs.map((run) => ({ ref: run.ref, name: run.name })),
      consumed: () => setRequest((now) => (now === req ? undefined : now)),
    };
    setRequest(req);
    reveal();
    return true;
  }, [port, reveal]);
  return { request, open };
}
