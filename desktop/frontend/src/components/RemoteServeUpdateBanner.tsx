import { useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { useRemoteStore } from "../store/remote";
import { dismissRemoteServeUpdate, isRemoteServeUpdateDismissed } from "../lib/remoteServeUpdateDismissal";

// Advisory drift banner for the active remote session: the old Serve keeps
// serving, the user decides. Upgrade arms in place (two clicks) because it
// interrupts in-flight remote turns; Ignore is remembered per serve version.
export function RemoteServeUpdateBanner({ hostId, workspace }: { hostId: string; workspace: string }) {
  const t = useT();
  const server = useRemoteStore((s) => s.servers[hostId]?.[workspace]);
  const [armedKey, setArmedKey] = useState<string | null>(null);
  const [dismissedKey, setDismissedKey] = useState<string | null>(null);
  const [failure, setFailure] = useState<{ key: string; message: string } | null>(null);
  if (!server?.updateAvailable || !server.serveVersion) return null;
  if (dismissedKey === scopeKey(hostId, workspace, server.serveVersion) || isRemoteServeUpdateDismissed(hostId, workspace, server.serveVersion)) return null;
  const armed = armedKey === scopeKey(hostId, workspace, server.serveVersion);
  const failed = failure?.key === scopeKey(hostId, workspace, server.serveVersion);
  const updating = server.state === "updating";
  return (
    <div className="banner banner--warning banner--actionable" data-testid="remote-serve-update-banner">
      <span className="banner__msg">
        {failed
          ? t("remote.serveUpdate.failed", { msg: failure!.message })
          : t("remote.serveUpdate.banner", { version: server.serveVersion })}
      </span>
      <span className="banner__spacer" />
      <button
        type="button"
        className={`btn btn--small${armed ? " btn--danger" : ""}`}
        disabled={updating}
        title={armed ? t("remote.serveUpdate.confirmTitle") : undefined}
        onClick={() => {
          if (updating) return;
          if (!armed) {
            setArmedKey(scopeKey(hostId, workspace, server.serveVersion!));
            return;
          }
          setArmedKey(null);
          setFailure(null);
          void app.UpdateRemoteServer(hostId, workspace).catch((error: unknown) => {
            const message = error instanceof Error ? error.message : String(error);
            setFailure({ key: scopeKey(hostId, workspace, server.serveVersion!), message: message.slice(0, 180) });
          });
        }}
      >
        {updating ? t("remote.serveUpdate.updating") : armed ? t("remote.serveUpdate.confirm") : t("remote.serveUpdate.update")}
      </button>
      <button
        type="button"
        className="btn btn--small"
        disabled={updating}
        onClick={() => {
          dismissRemoteServeUpdate(hostId, workspace, server.serveVersion!);
          setDismissedKey(scopeKey(hostId, workspace, server.serveVersion!));
        }}
      >
        {t("remote.serveUpdate.ignore")}
      </button>
    </div>
  );
}

// scopeKey keys the banner's transient state by identity, so switching
// between remotes running the same old version does not carry an armed
// confirmation, a dismissal, or a failure across hosts or workspaces.
function scopeKey(hostId: string, workspace: string, serveVersion: string): string {
  return `${hostId}|${workspace}|${serveVersion}`;
}
