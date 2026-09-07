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
import { createSubscriptionScope } from "../lib/subscriptionScope";

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

/** Owns app-wide bridge subscriptions; App regions never subscribe directly. */
export function AppRuntimeEffects(props: AppRuntimeEffectsProps) {
  const { onEvent: eventListener, onReady: readyListener, onRebuilt, onRemoteStatus: statusListener,
    onRemoteForwards: forwardsListener, onRemoteServer: serverListener,
    onInitialRemoteHosts, onInitialRemoteStatuses, running } = props;
  useEffect(startTerminalEventBridge, []);
  useEffect(() => {
    const scope = createSubscriptionScope(trackAppSubscription);
    scope.listen(onEvent, (event) => {
        eventListener(event);
        if (event.kind === "text" || event.kind === "reasoning" || event.kind === "tool_dispatch") {
          generativeMusic.playTokenNote();
        }
    });
    scope.listen(onReady, readyListener);
    scope.listen(onRuntimeRebuilt, onRebuilt);
    scope.listen(onRemoteStatus, statusListener);
    scope.listen(onRemoteForwards, forwardsListener);
    scope.listen(onRemoteServer, serverListener);
    return () => scope.dispose();
  }, [eventListener, readyListener, onRebuilt, forwardsListener, serverListener, statusListener]);

  useEffect(() => {
    let disposed = false;
    void app.RemoteHosts().then((hosts) => { if (!disposed) onInitialRemoteHosts(hosts); }).catch(() => {});
    void app.RemoteConnectionStatuses().then((statuses) => { if (!disposed) onInitialRemoteStatuses(statuses); }).catch(() => {});
    return () => { disposed = true; };
  }, [onInitialRemoteHosts, onInitialRemoteStatuses]);

  useEffect(() => {
    if (running && isGenerativeMusicEnabled()) generativeMusic.start();
    else generativeMusic.stop();
    return () => generativeMusic.stop();
  }, [running]);

  return null;
}
