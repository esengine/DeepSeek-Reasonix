import { useEffect, useState, type Dispatch, type SetStateAction } from "react";

export interface AgentTab { ref: string; name: string }
export interface AgentRequest { runs: AgentTab[]; consumed?: () => void }

/** The sub-agent runs open as workbench tabs. A request opens each run once and
 *  selects the first; the workbench reports it taken so a re-render never
 *  replays it. */
export function useAgentTabs(request: AgentRequest | undefined, pick: (key: string) => void, setDismissed: Dispatch<SetStateAction<Set<string>>>) {
  const [agents, setAgents] = useState<AgentTab[]>([]);
  useEffect(() => {
    if (!request || request.runs.length === 0) return;
    setAgents((now) => [...now, ...request.runs.filter((run) => !now.some((open) => open.ref === run.ref))]);
    setDismissed((now) => {
      const next = new Set(now);
      for (const run of request.runs) next.delete(`agent:${run.ref}`);
      return next;
    });
    pick(`agent:${request.runs[0].ref}`);
    request.consumed?.();
  }, [request, pick, setDismissed]);
  return { agents, setAgents };
}
