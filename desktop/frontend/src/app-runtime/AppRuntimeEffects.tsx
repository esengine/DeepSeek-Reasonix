import { useEffect } from "react";
import {
  app,
  onEvent,
  onReady,
  onRemoteForwards,
  onRemoteServer,
  onRemoteStatus,
  onRuntimeRebuilt,
} from "../lib/bridge";
import { generativeMusic, isGenerativeMusicEnabled } from "../lib/generative-music";
import { startTerminalEventBridge } from "../lib/terminalEvents";
import { trackAppSubscription } from "./appLifecycleProbe";

export type RuntimeEventListener = Parameters<typeof onEvent>[0];
export type RuntimeReadyListener = Parameters<typeof onReady>[0];
export type RuntimeRebuiltListener = Parameters<typeof onRuntimeRebuilt>[0];
export type RemoteStatusListener = Parameters<typeof onRemoteStatus>[0];
export type RemoteForwardsListener = Parameters<typeof onRemoteForwards>[0];
export type RemoteServerListener = Parameters<typeof onRemoteServer>[0];

type AppRuntimeEffectsProps = {
  running: boolean;
  onEvent: RuntimeEventListener;
  onReady: RuntimeReadyListener;
  onRebuilt: RuntimeRebuiltListener;
  onRemoteStatus: RemoteStatusListener;
  onRemoteForwards: RemoteForwardsListener;
  onRemoteServer: RemoteServerListener;
  onInitialRemoteHosts: (hosts: Awaited<ReturnType<typeof app.RemoteHosts>>) => void;
  onInitialRemoteStatuses: (statuses: Awaited<ReturnType<typeof app.RemoteConnectionStatuses>>) => void;
};

function tracked(unsubscribe: () => void): () => void {
  trackAppSubscription(1);
  return () => {
    unsubscribe();
    trackAppSubscription(-1);
  };
}

/** Owns app-wide bridge subscriptions; App regions never subscribe directly. */
export function AppRuntimeEffects(props: AppRuntimeEffectsProps) {
  useEffect(() => {
    startTerminalEventBridge();
    const stop = [
      tracked(onEvent((event) => {
        props.onEvent(event);
        if (event.kind === "text" || event.kind === "reasoning" || event.kind === "tool_dispatch") {
          generativeMusic.playTokenNote();
        }
      })),
      tracked(onReady(props.onReady)),
      tracked(onRuntimeRebuilt(props.onRebuilt)),
      tracked(onRemoteStatus(props.onRemoteStatus)),
      tracked(onRemoteForwards(props.onRemoteForwards)),
      tracked(onRemoteServer(props.onRemoteServer)),
    ];
    return () => stop.forEach((unsubscribe) => unsubscribe());
  }, [props.onEvent, props.onReady, props.onRebuilt, props.onRemoteForwards, props.onRemoteServer, props.onRemoteStatus]);

  useEffect(() => {
    let disposed = false;
    void app.RemoteHosts().then((hosts) => { if (!disposed) props.onInitialRemoteHosts(hosts); }).catch(() => {});
    void app.RemoteConnectionStatuses().then((statuses) => { if (!disposed) props.onInitialRemoteStatuses(statuses); }).catch(() => {});
    return () => { disposed = true; };
  }, [props.onInitialRemoteHosts, props.onInitialRemoteStatuses]);

  useEffect(() => {
    if (props.running && isGenerativeMusicEnabled()) generativeMusic.start();
    else generativeMusic.stop();
    return () => generativeMusic.stop();
  }, [props.running]);

  return null;
}
