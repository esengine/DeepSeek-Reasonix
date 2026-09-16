import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { ArrowDown } from "lucide-react";
import { getTranscriptStore } from "../lib/transcriptStore";
import type { ControllerLiveStore, HistoryLoadOutcome, HistoryLoadTrigger, Item, LiveStream } from "../lib/useController";
import { forkTargetForAnswer, type ForkBlockReason, type ForkTargetSetView, type ForkTargetView } from "../lib/forkTargets";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { ChatSource } from "../lib/chatViewSource";
import { ChatScrollController } from "../lib/chatScrollController";
import { ChatContentLoader } from "../lib/chatContentLoader";
import { ChatMountedOrder } from "../lib/chatMountedOrder";
import { ChatTurnJump } from "../lib/chatTurnJump";
import { findLoadedTurn, indexLoadedTurns, type LoadedTurnIndex } from "../lib/chatTurnRail";
import { getTranscriptOutlineStore } from "../lib/transcriptOutlineStore";
import { addBreadcrumb } from "../lib/breadcrumbs";
import { useT } from "../lib/i18n";
import { InvocationMetadataContext } from "./Message";
import { MarkdownImageTabContext } from "./MarkdownImageContext";
import { ChatFileScopeProvider } from "./ChatFileLinkContext";
import { ChatDetails, ChatNodeList, ChatRunning, type ChatActions } from "./ChatNodes";
import { Welcome } from "./Welcome";
import "./ChatTranscript.css";
const ChatTurnNavigator = lazy(() => import("./ChatTurnNavigator"));
export { NoticeCard } from "./TranscriptCards";

export type TranscriptProps = {
  items: Item[];
  live?: LiveStream;
  liveStore?: ControllerLiveStore;
  tabId?: string;
  hostId?: string;
  geometrySessionKey?: string;
  footerHeight?: number;
  onPrompt: (displayText: string, submitText?: string) => void;
  onFork?: (target: ForkTargetView) => void;
  /** Persisted fork boundaries of the shown session; undefined until the first read resolves. */
  forkTargets?: ForkTargetSetView;
  /** Non-null replaces every fork entry's own state, e.g. a surface that cannot create a child. */
  forkBlocked?: ForkBlockReason | null;
  running?: boolean;
  hydrating?: boolean;
  hasOlderHistory?: boolean;
  hasNewerHistory?: boolean;
  historyStartTurn?: number;
  historyEndTurn?: number;
  /** Total turns the snapshot reports, used to keep the rail area while the
   * outline loads without showing it on a brand-new conversation. */
  totalTurns?: number;
  loadingOlderHistory?: boolean;
  olderHistoryError?: string;
  onLoadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => HistoryLoadOutcome | boolean | Promise<HistoryLoadOutcome | boolean>;
  loadingNewerHistory?: boolean;
  newerHistoryError?: string;
  onLoadNewerHistory?: (latest?: boolean) => HistoryLoadOutcome | boolean | Promise<HistoryLoadOutcome | boolean>;
  turnStartAt?: number;
  invocationMetadata?: InvocationMetadataMap;
  surfaceCommitToken?: string;
  onSurfacePaintReady?: (token: string, outcome: "ready" | "degraded") => void;
};


/** Local and remote hosts share this natural-flow presentation adapter. */
export function Transcript(props: TranscriptProps) {
  const sessionKey = props.geometrySessionKey || `tab:${props.tabId ?? "preview"}`;
  return <ChatSession key={sessionKey} {...props} sessionKey={sessionKey} />;
}

function TranscriptConnection({ tabId }: { tabId?: string }) {
  const store = getTranscriptStore();
  const subscribe = useCallback((listener: () => void) => tabId ? store.subscribeState(tabId, listener) : () => {}, [store, tabId]);
  const snapshot = useCallback(() => tabId ? store.states.get(tabId)?.transcriptConnection : undefined, [store, tabId]);
  const status = useSyncExternalStore(subscribe, snapshot, snapshot);
  const t = useT();
  return status && status !== "connected" ? <p role="status">{t(status === "syncing" ? "chat.syncing" : "chat.disconnected")}</p> : null;
}

function ChatSession(props: TranscriptProps & { sessionKey: string }) {
  const { sessionKey, tabId, items, live, liveStore, running = false, hydrating = false,
    hasOlderHistory = false, hasNewerHistory = false, loadingOlderHistory = false, olderHistoryError,
    loadingNewerHistory = false, newerHistoryError, turnStartAt,
    onLoadOlderHistory, onLoadNewerHistory, onPrompt, onFork, onSurfacePaintReady, surfaceCommitToken } = props;
  const t = useT();
  const [source] = useState(() => new ChatSource(sessionKey));
  const [mounts] = useState(() => new ChatMountedOrder());
  const order = useSyncExternalStore(source.subscribeOrder, source.getOrderSnapshot, source.getOrderSnapshot);
  const [scroll] = useState(() => new ChatScrollController(sessionKey));
  const loader = useMemo(() => new ChatContentLoader(tabId), [tabId]);
  const scroller = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLElement | null>(null);
  const lifetime = useRef(0);
  const [details, setDetails] = useState<string>();
  const closeDetails = useCallback(() => { setDetails(undefined); }, []);
  const openDetails = useCallback((key: string, element: HTMLElement) => { trigger.current = element; setDetails(key); }, []);
  const recover = useCallback((id: string) => onPrompt(t("notice.protocolRecoveryAction"), `/recover-context ${id}`), [onPrompt, t]);
  const actions = useMemo<ChatActions>(() => ({ openDetails, recover,
    fork: onFork ? {
      targetFor: (answerKey) => forkTargetForAnswer(props.forkTargets, answerKey),
      loaded: props.forkTargets !== undefined,
      verifiable: props.forkTargets?.verifiable ?? false,
      blocked: props.forkBlocked ?? null,
      create: onFork,
    } : undefined }), [openDetails, recover, onFork, props.forkTargets, props.forkBlocked]);
  useLayoutEffect(() => {
    source.update({ items, live: props.hasNewerHistory ? undefined : liveStore?.getSnapshot(tabId) ?? live, running, hydrating,
      hasOlder: hasOlderHistory, loadingOlder: loadingOlderHistory, error: olderHistoryError,
      startedAt: turnStartAt, historyStartTurn: props.historyStartTurn });
  }, [source, items, live, liveStore, tabId, running, hydrating, hasOlderHistory, loadingOlderHistory, olderHistoryError, turnStartAt, props.historyStartTurn, props.hasNewerHistory]);
  useEffect(() => liveStore?.subscribe(tabId, () => source.updateLive(props.hasNewerHistory ? undefined : liveStore.getSnapshot(tabId))), [source, liveStore, tabId, props.hasNewerHistory]);
  useLayoutEffect(() => {
    if (scroller.current && column.current) scroll.attach(scroller.current, column.current);
    return () => scroll.dispose();
  }, [scroll]);
  useEffect(() => {
    loader.activate();
    acquireMarkdownWorkerClient();
    return () => { lifetime.current++; source.dispose(); mounts.dispose(); loader.dispose(); releaseMarkdownWorkerClient(); };
  }, [source, mounts, loader]);
  useLayoutEffect(() => {
    if (!hydrating) scroll.ready();
    scroll.layout();
  }, [scroll, items, hydrating, props.footerHeight, details]);
  useEffect(() => {
    if (hydrating || !surfaceCommitToken || (items.length > 0 && order.length === 0)) return;
    let paint = 0;
    const frame = requestAnimationFrame(() => { paint = requestAnimationFrame(() => onSurfacePaintReady?.(surfaceCommitToken, "ready")); });
    return () => { cancelAnimationFrame(frame); cancelAnimationFrame(paint); };
  }, [hydrating, surfaceCommitToken, onSurfacePaintReady, items.length, order.length]);
  const lastUser = [...items].reverse().find(item => item.kind === "user")?.id;
  const previousUser = useRef(lastUser);
  useLayoutEffect(() => {
    if (previousUser.current !== lastUser && running && !hydrating) scroll.toBottom();
    previousUser.current = lastUser;
  }, [lastUser, running, hydrating, scroll]);
  const position = useSyncExternalStore(scroll.subscribe, scroll.getSnapshot, scroll.getSnapshot);
  const activeDetails = details && source.getNodeSnapshot(details)?.kind === "tool" ? details : undefined;
  const drawerWasOpen = useRef(false);
  useLayoutEffect(() => {
    if (!activeDetails && drawerWasOpen.current) {
      (trigger.current?.isConnected ? trigger.current : scroller.current)?.focus({ preventScroll: true });
      trigger.current = null;
    }
    drawerWasOpen.current = Boolean(activeDetails);
  }, [activeDetails]);
  const [pagingError, setPagingError] = useState(false);
  const [selectionBlocked, setSelectionBlocked] = useState(false);
  // Manual paging and navigation jumps share one queue. A page already in
  // flight is awaited rather than submitted twice, so a jump that collides
  // with the button continues from that page instead of failing.
  const pagingPromise = useRef<Promise<HistoryLoadOutcome> | null>(null);
  const selectionInsideTranscript = () => {
    const selection = window.getSelection?.();
    return Boolean(selection && !selection.isCollapsed && scroller.current &&
      ((selection.anchorNode && scroller.current.contains(selection.anchorNode)) ||
        (selection.focusNode && scroller.current.contains(selection.focusNode))));
  };
  useEffect(() => {
    const clear = () => { if (!selectionInsideTranscript()) setSelectionBlocked(false); };
    document.addEventListener("selectionchange", clear);
    return () => document.removeEventListener("selectionchange", clear);
  }, []);
  const loadPage = (direction: "older" | "newer" | "latest", trigger: HistoryLoadTrigger = "viewport-user"): Promise<HistoryLoadOutcome> => {
    if (pagingPromise.current) return pagingPromise.current;
    const load = direction === "older" ? () => onLoadOlderHistory?.(undefined, trigger) : () => onLoadNewerHistory?.(direction === "latest");
    if (direction === "older" ? !onLoadOlderHistory : !onLoadNewerHistory) return Promise.resolve("empty");
    if (selectionInsideTranscript()) { setSelectionBlocked(true); return Promise.resolve("empty"); }
    const generation = lifetime.current;
    setPagingError(false);
    setSelectionBlocked(false);
    scroll.beforeChange();
    const run = (async (): Promise<HistoryLoadOutcome> => {
      try {
        // A host that still answers with a plain boolean is normalized here.
        const result = await load();
        if (result === true) return "loaded";
        if (result === false) return "empty";
        return result ?? "empty";
      } catch {
        if (generation === lifetime.current) setPagingError(true);
        return "empty";
      }
    })();
    pagingPromise.current = run;
    void run.finally(() => { if (pagingPromise.current === run) pagingPromise.current = null; });
    return run;
  };
  const loadOlder = (trigger: HistoryLoadTrigger = "viewport-user") => loadPage("older", trigger);
  // The jump outlives a single render, so it reads the live paging state
  // through refs rather than through the closure it was built with.
  const loadOlderRef = useRef(loadOlder); loadOlderRef.current = loadOlder;
  const hasOlderRef = useRef(false); hasOlderRef.current = hasOlderHistory && Boolean(onLoadOlderHistory);
  const lifetimeRef = useRef(lifetime.current); lifetimeRef.current = lifetime.current;
  // Rebuild the mounted identity index only when the mount advances, then
  // resolve each target from it in constant time: scanning the mounted order
  // per outline entry is quadratic on long conversations.
  const turnIndex = useRef<{ order: readonly string[]; index: LoadedTurnIndex }>(undefined);
  const jump = useMemo(() => new ChatTurnJump({
    mounts, scroll,
    loadOlder: () => loadOlderRef.current("question-jump"),
    hasOlder: () => hasOlderRef.current,
    resolveKey: (entry) => {
      const order = mounts.getSnapshot();
      if (turnIndex.current?.order !== order) {
        turnIndex.current = {
          order,
          index: indexLoadedTurns(order, (key) => {
            const node = source.getNodeSnapshot(key);
            return node?.kind === "user" ? { id: node.item.id, messageId: node.item.messageId } : undefined;
          }),
        };
      }
      return findLoadedTurn(turnIndex.current.index, entry);
    },
    // The rail describes one snapshot. A replacement invalidates the locators
    // this jump was resolved against, so it must not keep paging the new body.
    currentSnapshotId: () => (tabId ? getTranscriptOutlineStore().getView(tabId).snapshotId : ""),
    // Only a reader-initiated retry reaches this, and it is what lets a target
    // resolve against a fresh cut instead of the recycled one.
    refreshSnapshot: async (entry) => {
      if (!tabId) return undefined;
      const store = getTranscriptOutlineStore();
      await store.refresh(tabId);
      return store.resolve(tabId, entry);
    },
    isCurrent: () => lifetimeRef.current === lifetime.current,
  }), [mounts, scroll, source, tabId]);
  const jumpState = useSyncExternalStore(jump.subscribe, jump.getSnapshot, jump.getSnapshot);
  useEffect(() => () => jump.dispose(), [jump]);
  useEffect(() => {
    if (jumpState.status !== "failed") return;
    setPagingError(true);
    addBreadcrumb("chat.jump", `turn jump failed: ${jumpState.reason ?? "unknown"}`);
  }, [jumpState.status, jumpState.reason]);
  return <InvocationMetadataContext.Provider value={props.invocationMetadata ?? {}}>
    <MarkdownImageTabContext.Provider value={tabId ?? ""}>
      <ChatFileScopeProvider scopeKey={source.sessionKey} tabId={tabId} hostId={props.hostId}>
      <section className="chat-transcript">
        <div className="chat-surface" inert={Boolean(activeDetails)}>
          <Suspense fallback={null}><ChatTurnNavigator source={source} scroll={scroll} mounts={mounts}
            tabId={tabId} knownTurns={props.totalTurns ?? 0}
            busyTurn={jumpState.status === "loading" ? jumpState.turn : null}
            failedTurn={jumpState.status === "failed" ? jumpState.turn : null}
            failedReason={jumpState.reason}
            // Every click takes the one transaction entry point, so a newer
            // selection always supersedes a pending jump instead of racing it.
            onNavigate={(target) => {
              if (target.anchor.kind === "loaded") jump.jumpTo(target.anchor.key);
              else void jump.jump(target.entry);
            }}
            onRetryJump={() => { void jump.retry(); }}
            onCancelJump={() => jump.cancel()} /></Suspense>
          <div ref={scroller} className="transcript chat-flow-scroll" tabIndex={0} data-transcript-render-mode="full"
            data-transcript-hydrating={hydrating} data-scroll-mode={position.following ? "tail" : "reader"}>
            <div ref={column} className="chat-column">
              <TranscriptConnection tabId={tabId} />
              {hydrating && <p role="status">{t("chat.loading")}</p>}
              {(hasOlderHistory || hasNewerHistory) && <div className="chat-history-window" role="status">
                <span>{t("chat.historyRange", { start: Math.max(1, (props.historyStartTurn ?? 0) + 1), end: Math.max(1, props.historyEndTurn ?? props.totalTurns ?? 0), total: props.totalTurns ?? 0 })}</span>
              </div>}
              {hasOlderHistory && <button className="btn chat-older" disabled={loadingOlderHistory} onClick={() => void loadOlder()}>{t(loadingOlderHistory ? "chat.loading" : "chat.loadOlder")}</button>}
              {(olderHistoryError || pagingError) && <button className="btn" onClick={() => void loadOlder()}>{t("chat.loadFailed")}</button>}
              {selectionBlocked && <p className="chat-history-selection" role="status">{t("chat.historySelectionBlocked")}</p>}
              {!hydrating && items.length === 0 && !running && <Welcome onPrompt={onPrompt} />}
              <ChatNodeList key={source.sessionKey} source={source} mounts={mounts} loader={loader} scroll={scroll} actions={actions} tabId={tabId} hostId={props.hostId} />
              <ChatRunning source={source} />
              {hasNewerHistory && <div className="chat-history-newer">
                <button className="btn" disabled={loadingNewerHistory} onClick={() => void loadPage("newer")}>{t(loadingNewerHistory ? "chat.loading" : "chat.loadNewer")}</button>
                <button className="btn" disabled={loadingNewerHistory} onClick={() => void loadPage("latest")}>{t("chat.toLatest")}</button>
              </div>}
              {newerHistoryError && <button className="btn" onClick={() => void loadPage("newer")}>{t("chat.loadFailed")}</button>}
            </div>
          </div>
          <button className="btn chat-to-bottom" hidden={position.following} aria-label={t("chat.toLatest")} onClick={scroll.toBottom}><ArrowDown size={18} /></button>
        </div>
        {activeDetails && <ChatDetails key={activeDetails} source={source} nodeKey={activeDetails} loader={loader} onClose={closeDetails} onNavigate={setDetails} />}
      </section>
      </ChatFileScopeProvider>
    </MarkdownImageTabContext.Provider>
  </InvocationMetadataContext.Provider>;
}
