import { useCallback, useRef, useState } from "react";
import { app } from "./bridge";
import {
  beginKeyedResourceRequest,
  emptyKeyedResource,
  rejectKeyedResourceRequest,
  resolveKeyedResourceRequest,
} from "./keyedResource";
import type { WorkspaceChangesView } from "./types";

function errorMessage(error: unknown): string {
  if (typeof error === "object" && error && "message" in error) {
    return String((error as { message?: unknown }).message ?? error);
  }
  return String(error);
}

export function useWorkspaceChangesResource(tabId: string, scopeKey: string, revision: number) {
  const [resource, setResource] = useState(() => emptyKeyedResource<WorkspaceChangesView>());
  const requestSeq = useRef(0);
  const key = `${scopeKey}\u0000working-tree`;

  const load = useCallback(async () => {
    const requestId = ++requestSeq.current;
    const requestKey = `${scopeKey}\u0000working-tree`;
    setResource((current) => beginKeyedResourceRequest(current, requestKey, requestId, revision));
    try {
      const result = await app.WorkspaceChanges(tabId);
      const next = {
        files: Array.isArray(result?.files) ? result.files : [],
        gitAvailable: result?.gitAvailable !== false,
        gitErr: result?.gitErr,
        gitBranch: result?.gitBranch,
      };
      setResource((current) => resolveKeyedResourceRequest(current, requestKey, requestId, next, revision));
    } catch (error) {
      setResource((current) => rejectKeyedResourceRequest(current, requestKey, requestId, errorMessage(error)));
    }
  }, [revision, scopeKey, tabId]);

  const reset = useCallback(() => {
    requestSeq.current += 1;
    setResource(emptyKeyedResource());
  }, []);

  const current = resource.key === key;
  return {
    workspaceChanges: current ? resource.data : null,
    loadingWorkspaceChanges: current && resource.status === "refreshing",
    workspaceChangesErr: current ? resource.error : "",
    loadWorkspaceChanges: load,
    resetWorkspaceChanges: reset,
  };
}
