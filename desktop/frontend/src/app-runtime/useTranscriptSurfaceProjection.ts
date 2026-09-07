import { useLayoutEffect, useMemo } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { useNavigationSurface } from "../lib/useNavigationSurface";
import type { HistoryLoadTrigger, Item } from "../lib/useController";

type NavigationSurfaceApi = ReturnType<typeof useNavigationSurface>;

export type TranscriptSurfaceProjectionInput = {
  hydrating: boolean;
  hydrateHistoryLoaded: boolean | undefined;
  hydratePlaceholderItems: Item[] | undefined;
  hydratePlaceholderActive: boolean;
  items: Item[];
  remote: boolean;
  remoteItems: Item[];
  activeTabId: string | undefined;
  geometrySessionKey: string;
  transitioning: boolean;
  navigationDataReady: boolean;
  preserved: NavigationSurfaceApi["preserved"];
  singleSurface: boolean;
  controllerReady: boolean;
  creationLayout: boolean;
  imDetailActive: boolean;
  sessionHasContent: boolean;
  commitRendered: NavigationSurfaceApi["commitRendered"];
  commitPaint: NavigationSurfaceApi["commitPaint"];
  commitSingleSurface: (tabId: string) => void;
  ports: {
    loadOlderHistory(tabId: string, targetTurn: number | undefined, trigger: HistoryLoadTrigger): Promise<boolean>;
    commitThenSend(tabId: string, text: string): Promise<void>;
  };
};

/**
 * Owns the transcript surface projection: hydration placeholders, the
 * creation empty hero gate, the committed-surface commit effect (only
 * committed presentation may become a retained source), the visible
 * source-retained surface selection, surface paint receipts, the latest
 * consumed guidance entry and the transcript prompt/load-older commands.
 */
export function useTranscriptSurfaceProjection(input: TranscriptSurfaceProjectionInput) {
  const { activeTabId, transitioning, ports } = input;
  const transcriptHydrating = input.hydrating && !input.hydrateHistoryLoaded;
  // Creation hero only after history hydration settles on a truly empty session.
  // Avoid flash while switching tabs: items may be empty while placeholders show.
  // Exclude IM/Bot detail: hero CSS collapses .main, which also hosts that panel.
  const creationEmptyHero =
    input.creationLayout &&
    !transitioning &&
    !input.imDetailActive &&
    !input.sessionHasContent &&
    !transcriptHydrating &&
    !input.hydratePlaceholderActive;
  const transcriptItems = input.hydratePlaceholderActive ? input.hydratePlaceholderItems! : input.items;
  const handleLoadOlderHistory = useCommittedCommand((targetTurn?: number, trigger: HistoryLoadTrigger = "retry") => {
    return activeTabId ? ports.loadOlderHistory(activeTabId, targetTurn, trigger) : Promise.resolve(false);
  });

  // Display items: backend history is authoritative after immediate commit.
  // rewindState only drives the undo banner, not optimistic truncation.
  const displayItems = transcriptItems;
  const committedSurfaceItems = input.remote ? input.remoteItems : displayItems;
  const committedGeometryKey = input.remote ? `tab:${activeTabId ?? "preview"}` : input.geometrySessionKey;
  // Only committed presentation can become a future retained source surface.
  // A suspended or abandoned render must never become navigation authority.
  const commitRendered = input.commitRendered;
  useLayoutEffect(() => {
    if (transitioning) return;
    commitRendered({
      tabId: activeTabId,
      items: committedSurfaceItems,
      geometrySessionKey: committedGeometryKey,
    });
  }, [activeTabId, commitRendered, committedSurfaceItems, transitioning, committedGeometryKey]);
  const visibleTranscriptSurface = transitioning && !input.navigationDataReady && input.preserved
    ? input.preserved
    : null;
  const visibleTranscriptItems = visibleTranscriptSurface?.items ?? displayItems;
  const visibleTranscriptTabId = visibleTranscriptSurface?.tabId ?? activeTabId;
  const visibleTranscriptGeometryKey = visibleTranscriptSurface?.geometrySessionKey ?? input.geometrySessionKey;
  const handleSurfacePaintReady = useCommittedCommand((token: string, outcome: "ready" | "degraded") => {
    const receipt = input.commitPaint(token, outcome);
    if (input.singleSurface && receipt) input.commitSingleSurface(receipt.targetTabId);
  });
  const latestGuidanceConsumed = useMemo(() => {
    for (let i = input.items.length - 1; i >= 0; i--) {
      const item = input.items[i];
      if (item.kind === "notice" && item.text.startsWith("↪ ")) {
        return { key: item.id, itemId: item.inboxItemId, text: item.text.slice(2) };
      }
    }
    return null;
  }, [input.items]);

  const handleTranscriptPrompt = useCommittedCommand((text: string) => {
    if (!activeTabId || !input.controllerReady) return;
    void ports.commitThenSend(activeTabId, text).catch((err) => {
      console.warn("Failed to submit transcript prompt", err);
    });
  });

  return {
    transcriptHydrating,
    creationEmptyHero,
    visibleTranscriptItems,
    visibleTranscriptTabId,
    visibleTranscriptGeometryKey,
    handleLoadOlderHistory,
    handleSurfacePaintReady,
    latestGuidanceConsumed,
    handleTranscriptPrompt,
  };
}
