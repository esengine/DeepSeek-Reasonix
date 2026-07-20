import { Loader2, Server } from "lucide-react";

import { useT } from "../lib/i18n";
import type { RemoteTargetStatusView } from "../lib/types";
import { Tooltip } from "./Tooltip";

type RemoteToolbarTone = "local" | "connected" | "transition" | "attention";

function targetStateKey(state: RemoteTargetStatusView["state"]) {
  return `remote.state.${state}` as const;
}

export function remoteToolbarTone(status: RemoteTargetStatusView | null): RemoteToolbarTone {
  if (!status) return "local";
  if (status.failure || status.state === "Disconnected") return "attention";
  if (status.state === "RemoteConnecting" || status.state === "RemoteReconnecting" || status.state === "Switching") {
    return "transition";
  }
  if (status.canReconnect) return "attention";
  if (status.state === "RemoteConnected") return "connected";
  return "local";
}

export function RemoteTargetToolbarButton({
  status,
  onOpen,
}: {
  status: RemoteTargetStatusView | null;
  onOpen: () => void;
}) {
  const t = useT();
  const tone = remoteToolbarTone(status);
  const label = !status || (status.state === "LocalConnected" && !status.failure && !status.canReconnect)
    ? t("remote.toolbar.connect")
    : [t(targetStateKey(status.state)), status.hostLabel].filter(Boolean).join(" · ");

  return (
    <Tooltip label={label}>
      <button
        className={`topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility remote-target-toolbar-btn remote-target-toolbar-btn--${tone}`}
        type="button"
        aria-label={label}
        data-remote-state={status?.state ?? "Unknown"}
        onClick={onOpen}
      >
        {tone === "transition"
          ? <Loader2 className="spin" size={14} aria-hidden="true" />
          : <Server size={14} aria-hidden="true" />}
        <span className="remote-target-toolbar-btn__indicator" aria-hidden="true" />
      </button>
    </Tooltip>
  );
}
