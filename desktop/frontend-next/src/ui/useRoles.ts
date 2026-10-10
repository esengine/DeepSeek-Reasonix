import { useCallback, useState } from "react";
import type { AgentPort, RoleAssignments, RoleOverride } from "../port/port";

// The global assignment and the entries that outrank it are two reads of one
// subject; showing the first without the second is how a setting appears to do
// nothing.
export function useRoles(port: AgentPort) {
  const [roles, setRoles] = useState<RoleAssignments | null>(null);
  const [overrides, setOverrides] = useState<Record<string, RoleOverride[]>>({});
  const loadRoles = useCallback(() => {
    port.roles().then(setRoles).catch(() => setRoles(null));
    port.roleOverrides().then(setOverrides).catch(() => setOverrides({}));
  }, [port]);
  return { roles, overrides, loadRoles };
}
