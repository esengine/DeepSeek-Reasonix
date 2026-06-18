import type { ServerView } from "./types";

export function mcpServerLifecycleActions(s: ServerView): {
  enabled: boolean;
  showRetryInRow: boolean;
  canConnectNow: boolean;
  canReconnect: boolean;
} {
  return {
    enabled: s.status === "connected" || s.status === "deferred" || s.status === "initializing",
    showRetryInRow: s.status === "failed" || s.status === "initializing",
    canConnectNow: s.status === "deferred" || s.status === "disabled",
    canReconnect: s.status === "connected" || s.status === "initializing",
  };
}
