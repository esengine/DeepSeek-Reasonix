import React, { useRef, type ReactNode } from "react";
import { useDesktopNavigation } from "../../app-runtime/useDesktopNavigation";
import { RemoteNavigationContext } from "../../lib/remoteNavigationCommands";
import { app } from "../../lib/bridge";
import { useNavigationIntentFence } from "../../lib/useNavigationIntentFence";
import { useT } from "../../lib/i18n";

const noop = () => {};
const unavailable = async (): Promise<never> => { throw new Error("unexpected local navigation in remote fixture"); };
/** Component fixtures use the production owner and registration fence, not a second navigation implementation. */
export function RemoteNavigationHarness({ children }: { children: ReactNode }) {
  const sequence = useRef(0);
  const fence = useNavigationIntentFence();
  const { openRemoteProject } = useDesktopNavigation({ visible: { tabId: "fixture", sessionKey: "fixture" }, singleSurface: true,
    noteIntent: () => { const seq = ++sequence.current; fence.registerNavigationIntent(seq); return seq; },
    beginSurface: noop, settleSurface: noop, showChat: noop,
    setTabRevealSignal: noop, setTranscriptRevealSignal: noop, setProjectRevision: noop, setHistory: noop, t: useT(), showToast: noop,
    ports: { registeredNavigationIntent: fence.registeredNavigationIntent, isNavigationIntentCurrent: seq => seq === sequence.current,
      openRemoteProject: app.OpenRemoteProjectTab, switchRemoteTab: async () => {},
      activateTopic: unavailable, openTopicSession: unavailable, openGlobalTab: unavailable, openProjectTab: unavailable,
      ensureBlankSurface: unavailable, ensureBlankTab: unavailable, createIsolatedWorktree: unavailable,
      openChannelSession: unavailable, resumeSession: unavailable,
      listTabs: async () => [], listSessions: async () => [], applyTabs: noop, seedTab: noop,
    },
  });
  return <RemoteNavigationContext.Provider value={openRemoteProject}>{children}</RemoteNavigationContext.Provider>;
}
