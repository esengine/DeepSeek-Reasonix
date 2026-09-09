import { useMemo } from "react";
import { useResourceOperations, type SessionResource } from "./useResourceOperations";
export type { SessionResource, SessionOperationAuthority } from "./useResourceOperations";

function bindSessions(operations: ReturnType<typeof useResourceOperations>) {
  return <Input, Result>(target: SessionResource, channel: string, input: Input,
    execute: (input: Input, authority: import("./useResourceOperations").SessionOperationAuthority) => Result) => (
    operations({ kind: "session", ...target }, channel, input, execute)
  );
}

export function useSessionOperations(input: { visible: SessionResource; resources: readonly SessionResource[] }) {
  const operations = useResourceOperations({ visible: input.visible, resources: input.resources.map(resource => ({ kind: "session", ...resource })) });
  return useMemo(() => bindSessions(operations), [operations]);
}
