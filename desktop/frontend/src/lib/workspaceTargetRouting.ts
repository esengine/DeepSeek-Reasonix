import type { RemoteTargetStatusView } from "./types";

export type WorkspaceCreationRoute = "local-picker" | "remote-setup" | "blocked";

// ProjectTree has several "add/open folder" entry points, but they must all
// resolve against TargetManager state before choosing a filesystem surface.
// Only a fully connected Local target may open the Desktop directory picker.
export function workspaceCreationRoute(status: RemoteTargetStatusView): WorkspaceCreationRoute {
  if (status.state === "LocalConnected") return "local-picker";
  if (status.state === "RemoteConnected") return "remote-setup";
  return "blocked";
}
