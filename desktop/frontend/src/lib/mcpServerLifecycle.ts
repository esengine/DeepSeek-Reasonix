import type { ServerView } from "./types";

function isEnabled(s: ServerView): boolean {
  if (typeof s.enabled === "boolean") return s.enabled;
  if (s.startIntent === "off" || s.status === "disabled") return false;
  if (s.configured && s.autoStart === false) return false;
  return true;
}

function runtimeState(s: ServerView): "idle" | "connecting" | "ready" | "issue" {
  if (s.runtimeState === "idle" || s.runtimeState === "connecting" || s.runtimeState === "ready" || s.runtimeState === "issue") {
    return s.runtimeState;
  }
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
  const enabled = isEnabled(s);
  const state = runtimeState(s);
  return {
    enabled: state === "ready" || enabled,
    showRetryInRow: state === "issue" || s.action === "retry",
    canConnectNow: !enabled && state !== "ready",
    canReconnect: state === "ready" || state === "issue",
  };
}

export function mcpServerRetryableFromAvailableList(s: ServerView): boolean {
	if (s.status === "connected" || s.status === "disabled" || s.status === "failed") return false;
	if (!isEnabled(s)) return false;
	return runtimeState(s) === "connecting" || s.action === "retry";
}

/** Prefer product availability labels over legacy status strings. */
export function mcpServerAvailability(s: ServerView): string {
  if (s.availability) return s.availability;
  if (!isEnabled(s)) return "disabled";
  switch (runtimeState(s)) {
    case "ready":
      return "connected";
    case "connecting":
      return "starting";
    case "issue":
      if (s.requiresLaunchApproval) return "project_auth_changed";
      if (s.authStatus === "required" || s.authStatus === "possible") return "auth_required";
      return "start_failed";
    default:
      return "available_on_demand";
  }
}
