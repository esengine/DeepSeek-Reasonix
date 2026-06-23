import type { ServerView } from "./types";

function startIntent(s: ServerView): "off" | "automatic" {
  if (s.startIntent === "off" || s.startIntent === "automatic") return s.startIntent;
  if (s.status === "disabled" || (s.configured && !s.autoStart)) return "off";
  return "automatic";
}

function runtimeState(s: ServerView): "idle" | "connecting" | "ready" | "issue" {
  if (s.runtimeState === "idle" || s.runtimeState === "connecting" || s.runtimeState === "ready" || s.runtimeState === "issue") return s.runtimeState;
  if (s.status === "connected") return "ready";
  if (s.status === "initializing") return "connecting";
  if (s.status === "failed") return "issue";
  return "idle";
}

export function mcpServerLifecycleActions(s: ServerView): {
  enabled: boolean;
  showRetryInRow: boolean;
  canConnectNow: boolean;
  canReconnect: boolean;
} {
  const intent = startIntent(s);
  const state = runtimeState(s);
  return {
    enabled: state === "ready" || intent !== "off",
    showRetryInRow: state === "issue",
    canConnectNow: intent === "off" && state !== "ready",
    canReconnect: state === "ready" || state === "issue",
  };
}

export function mcpServerRetryableFromAvailableList(s: ServerView): boolean {
  if (s.status === "connected" || s.status === "disabled" || s.status === "failed") return false;
  return startIntent(s) !== "off";
}
