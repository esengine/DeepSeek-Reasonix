import { useRef } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { RemoteConnectionTimeoutError, useRemoteStore, waitForRemoteConnection } from "../store/remote";
import { RemoteWorkspaceLaunchGate, resolveRemoteWorkspace } from "../lib/remoteWorkspace";
import { publishNavigationIntent } from "../lib/useNavigationIntentFence";
import type { RemoteHostView } from "../lib/types";
import type { Translator } from "../lib/i18n";

export type RemoteWorkspaceCommandsInput = {
  t: Translator;
  showToast(message: string, level: "error", options?: { durationMs?: number; actionLabel?: string; onAction?: () => void }): void;
};

/**
 * Owns remote workspace launches and host connections. Each host gets one
 * launch generation; a status popover entry may open the workspace, and a
 * connect request first drives the host to connected (clearing stale failure
 * state, then waiting on the connection waiter) before launching. A timeout
 * offers stop-and-retry; other failures stay host-scoped on the status entry.
 */
export function useRemoteWorkspaceCommands(input: RemoteWorkspaceCommandsInput) {
  const { t, showToast } = input;
  const remoteWorkspaceLaunchGate = useRef(new RemoteWorkspaceLaunchGate());

  const launchRemoteWorkspace = useCommittedCommand(async (host: RemoteHostView, requestSeq: number) => {
    const lastWorkspace = await app.RemoteLastWorkspace(host.id).catch(() => "");
    const workspace = resolveRemoteWorkspace(lastWorkspace, host.defaultWorkspace);
    if (!remoteWorkspaceLaunchGate.current.isCurrent(host.id, requestSeq)) return;
    await publishNavigationIntent("remote-workspace");
    await app.OpenRemoteWorkspace(host.id, workspace);
  });

  const openRemoteWorkspaceFromStatus = useCommittedCommand((host: RemoteHostView) => {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void launchRemoteWorkspace(host, requestSeq).catch((err) => {
      showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
    });
  });

  const connectAndOpenRemoteWorkspace = useCommittedCommand(function connectRemoteWorkspace(host: RemoteHostView) {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void (async () => {
      try {
        const status = useRemoteStore.getState().statuses[host.id]?.state;
        if (status !== "connected" && status !== "degraded") {
          // Clear any stale failure before the new generation starts; otherwise a
          // previous stopped+error snapshot could make the waiter reject before
          // the kernel's fresh connecting event reaches the frontend.
          useRemoteStore.getState().applyStatus({ hostId: host.id, state: "connecting" });
          await app.ConnectRemoteHost(host.id);
          await waitForRemoteConnection(host.id);
        }
      } catch (err) {
        if (err instanceof RemoteConnectionTimeoutError) {
          showToast(t("remote.error.timeout", { host: host.label }), "error", {
            actionLabel: t("remote.error.stopAndRetry"),
            durationMs: 10_000,
            onAction: () => {
              void app.DisconnectRemoteHost(host.id)
                .catch(() => undefined)
                .then(() => connectRemoteWorkspace(host));
            },
          });
          return;
        }
        // Connection failures are host-scoped. Keep the persistent error and its
        // recovery actions beside the Remote SSH status entry instead of
        // stretching a raw backend error across the native titlebar.
        useRemoteStore.getState().requestStatusPopover(host.id);
        return;
      }

      try {
        await launchRemoteWorkspace(host, requestSeq);
      } catch (err) {
        showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
      }
    })();
  });

  return { openRemoteWorkspaceFromStatus, connectAndOpenRemoteWorkspace };
}
