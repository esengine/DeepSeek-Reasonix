import { useEffect, useState } from "react";
import type { AgentPort, WorkspaceInfo } from "../port/port";

export function useWorkspaceInfo(port: AgentPort | null, root: string, ready: boolean) {
  const [read, setRead] = useState<{ port: AgentPort; root: string; value: WorkspaceInfo } | null>(null);
  useEffect(() => {
    if (!port || !root || !ready) return;
    let current = true;
    setRead(null);
    void port.workspaces().then((value) => {
      if (current) setRead({ port, root, value });
    }).catch(() => {});
    return () => { current = false; };
  }, [port, root, ready]);
  return ready && read?.port === port && read.root === root ? read.value : null;
}
