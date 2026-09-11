import type { State } from "./useController";
import type { RemoteSessionApi } from "./useRemoteSession";

export type SessionAvailability = {
  kind: "ready" | "loading" | "error";
  source: "history" | "connection" | "runtime";
  detail?: string;
};

type LocalSession = Pick<State, "meta" | "backendActivationPending" | "hydrating" | "hydrateError">;
type RemoteSession = Pick<RemoteSessionApi, "state" | "hydrated" | "error">;

/** Navigation, welcome and recovery must agree on the active source's readiness. */
export function projectSessionAvailability(input: { local?: LocalSession; remote?: RemoteSession }): SessionAvailability {
  const { local, remote } = input;
  if (remote) {
    if (["error", "serve_down", "disconnected"].includes(remote.state)) {
      return { kind: "error", source: "connection", detail: remote.error };
    }
    if (!remote.hydrated && remote.error) return { kind: "error", source: "history", detail: remote.error };
    if (remote.state !== "ready") return { kind: "loading", source: "connection" };
    return { kind: remote.hydrated ? "ready" : "loading", source: "history" };
  }
  if (local?.hydrateError) return { kind: "error", source: "history", detail: local.hydrateError };
  if (local?.meta?.startupErr) return { kind: "error", source: "runtime", detail: local.meta.startupErr };
  if (local?.hydrating) return { kind: "loading", source: "history" };
  const ready = local?.meta?.ready === true && !local.backendActivationPending
    && (!local.meta.runtime || local.meta.runtime.phase === "ready");
  return { kind: ready ? "ready" : "loading", source: "runtime" };
}
