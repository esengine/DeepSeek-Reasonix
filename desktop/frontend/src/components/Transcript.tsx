import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { ArrowDown } from "lucide-react";
import type { ControllerLiveStore, HistoryLoadTrigger, Item, LiveStream } from "../lib/useController";
import type { CheckpointMeta } from "../lib/types";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";
import { acquireMarkdownWorkerClient, releaseMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { ChatSource } from "../lib/chatViewSource";
import { ChatScrollController } from "../lib/chatScrollController";
import { ChatContentLoader } from "../lib/chatContentLoader";
import { useT } from "../lib/i18n";
import { InvocationMetadataContext } from "./Message";
import { MarkdownImageTabContext } from "./MarkdownImageContext";
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
  onFork?: (turn: number) => void;
  checkpoints?: CheckpointMeta[];
  actionPending?: boolean;
  rewindDisabled?: boolean;
  running?: boolean;
  hydrating?: boolean;
  hasOlderHistory?: boolean;
  historyStartTurn?: number;
  loadingOlderHistory?: boolean;
  olderHistoryError?: string;
  onLoadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => boolean | Promise<boolean>;
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

function ChatSession(props: TranscriptProps & { sessionKey: string }) {
  const { sessionKey, tabId, items, live, liveStore, running = false, hydrating = false,
    hasOlderHistory = false, loadingOlderHistory = false, olderHistoryError, turnStartAt,
    onLoadOlderHistory, onPrompt, onFork, onSurfacePaintReady, surfaceCommitToken } = props;
  const t = useT();
  const [source] = useState(() => new ChatSource(sessionKey));
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
  const actions = useMemo<ChatActions>(() => ({ openDetails, recover, fork: onFork,
    forkDisabled: Boolean(props.rewindDisabled || props.actionPending || running || hydrating),
    checkpoints: props.checkpoints ?? [] }), [openDetails, recover, onFork, props.rewindDisabled, props.actionPending, props.checkpoints, running, hydrating]);
  useLayoutEffect(() => {
    source.update({ items, live: liveStore?.getSnapshot(tabId) ?? live, running, hydrating,
      hasOlder: hasOlderHistory, loadingOlder: loadingOlderHistory, error: olderHistoryError,
      startedAt: turnStartAt, historyStartTurn: props.historyStartTurn });
  }, [source, items, live, liveStore, tabId, running, hydrating, hasOlderHistory, loadingOlderHistory, olderHistoryError, turnStartAt, props.historyStartTurn]);
  useEffect(() => liveStore?.subscribe(tabId, () => source.updateLive(liveStore.getSnapshot(tabId))), [source, liveStore, tabId]);
  useLayoutEffect(() => {
    if (scroller.current && column.current) scroll.attach(scroller.current, column.current);
    return () => scroll.dispose();
  }, [scroll]);
  useEffect(() => {
    loader.activate();
    acquireMarkdownWorkerClient();
    return () => { lifetime.current++; source.dispose(); loader.dispose(); releaseMarkdownWorkerClient(); };
  }, [source, loader]);
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
  const paging = useRef(false);
  const loadOlder = async () => {
    if (paging.current || !onLoadOlderHistory) return;
    const generation = lifetime.current;
    paging.current = true; setPagingError(false); scroll.beforeChange();
    try { await onLoadOlderHistory(undefined, "viewport-user"); }
    catch { if (generation === lifetime.current) setPagingError(true); }
    finally { if (generation === lifetime.current) paging.current = false; }
  };
  return <InvocationMetadataContext.Provider value={props.invocationMetadata ?? {}}>
    <MarkdownImageTabContext.Provider value={tabId ?? ""}>
      <section className="chat-transcript">
        <div className="chat-surface" inert={Boolean(activeDetails)}>
          <Suspense fallback={null}><ChatTurnNavigator source={source} scroll={scroll} /></Suspense>
          <div ref={scroller} className="transcript chat-flow-scroll" tabIndex={0} data-transcript-render-mode="full"
            data-transcript-hydrating={hydrating} data-scroll-mode={position.following ? "tail" : "reader"}>
            <div ref={column} className="chat-column">
              {hydrating && <p role="status">{t("chat.loading")}</p>}
              {hasOlderHistory && <button className="btn chat-older" disabled={loadingOlderHistory} onClick={() => void loadOlder()}>{t(loadingOlderHistory ? "chat.loading" : "chat.loadOlder")}</button>}
              {(olderHistoryError || pagingError) && <button className="btn" onClick={() => void loadOlder()}>{t("chat.loadFailed")}</button>}
              {!hydrating && items.length === 0 && !running && <Welcome onPrompt={onPrompt} />}
              <ChatNodeList key={source.sessionKey} source={source} loader={loader} scroll={scroll} actions={actions} tabId={tabId} hostId={props.hostId} />
              <ChatRunning source={source} />
            </div>
          </div>
          <button className="btn chat-to-bottom" hidden={position.following} aria-label={t("chat.toLatest")} onClick={scroll.toBottom}><ArrowDown size={18} /></button>
        </div>
        {activeDetails && <ChatDetails key={activeDetails} source={source} nodeKey={activeDetails} loader={loader} onClose={closeDetails} onNavigate={setDetails} />}
      </section>
    </MarkdownImageTabContext.Provider>
  </InvocationMetadataContext.Provider>;
}
