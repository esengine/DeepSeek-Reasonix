import { lazy, Suspense } from "react";
import { Transcript, type TranscriptProps } from "../components/Transcript";
import { NoticePreviewPanel, noticePreviewMockEnabled } from "./NoticePreviewPanel";
import type { SidebarImConnection } from "../app-runtime/sidebarImProjection";
import type { TabMeta } from "../lib/types";
import type { State } from "../lib/useController";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import type { Translator } from "../lib/i18n";

const RemoteSessionSurface = lazy(() => import("../components/RemoteSessionSurface").then((module) => ({ default: module.RemoteSessionSurface })));
const SidebarImConnectionDetail = lazy(() => import("./SidebarImConnectionDetail").then((module) => ({ default: module.SidebarImConnectionDetail })));

export type ChatPaneTranscriptInput = {
  state: State;
  items: TranscriptProps["items"];
  tabId: TranscriptProps["tabId"];
  geometrySessionKey: TranscriptProps["geometrySessionKey"];
  footerHeight: TranscriptProps["footerHeight"];
  revealSignal: TranscriptProps["revealSignal"];
  invocationMetadata: TranscriptProps["invocationMetadata"];
  surfaceCommitToken: TranscriptProps["surfaceCommitToken"];
  liveStore: TranscriptProps["liveStore"];
  transcriptHydrating: boolean;
  navigationDataReady: boolean;
  readOnly: boolean;
  controllerReady: boolean;
  hydratePlaceholderActive: boolean;
  clearContextPending: boolean;
  creation: boolean;
  rewind: {
    stateActive: boolean;
    committing: boolean;
    signal: TranscriptProps["rewindSignal"];
  };
};

export type ChatPaneRegionProps = {
  transitioning: boolean;
  t: Translator;
  imDetail: {
    connection: SidebarImConnection;
    onClose: () => void;
    onOpenSettings: () => void;
    onManageAllowlist: (connectionId: string) => void;
    onOpenSession: (connection: SidebarImConnection) => void;
  } | null;
  remote: { tab: TabMeta; session: RemoteSessionApi } | undefined;
  transcript: ChatPaneTranscriptInput;
  onRetryHistory: () => void;
  commands: {
    onPrompt: TranscriptProps["onPrompt"];
    onDeliveryContinue: TranscriptProps["onDeliveryContinue"];
    onAcceptDelivery: TranscriptProps["onAcceptDelivery"];
    onOpenChanges: TranscriptProps["onOpenChanges"];
    onOpenVerification: TranscriptProps["onOpenVerification"];
    onEditPrompt: TranscriptProps["onEditPrompt"];
    onRewind: TranscriptProps["onRewind"];
    onLoadOlderHistory: TranscriptProps["onLoadOlderHistory"];
    onSurfacePaintReady: TranscriptProps["onSurfacePaintReady"];
  };
};

/**
 * The chat-pane main surface: IM/bot detail, notice preview mock, remote
 * session surface or the local transcript with its navigation-transition
 * wrapper and history-load error. Pure prop-driven; all ownership stays in
 * the caller's owners.
 */
export function ChatPaneRegion(props: ChatPaneRegionProps) {
  const { transitioning, t, transcript, commands } = props;
  const { state, rewind } = transcript;
  const rewindDisabled = transcript.readOnly || !transcript.controllerReady || transcript.hydratePlaceholderActive
    || rewind.stateActive || rewind.committing || state.running
    || state.messageAction != null || state.approval != null || state.ask != null
    || transcript.clearContextPending || transitioning;
  return (
    <main className="main">
      {props.imDetail && !transitioning ? (
        <SidebarImConnectionDetail
          connection={props.imDetail.connection}
          onClose={props.imDetail.onClose}
          onOpenSettings={props.imDetail.onOpenSettings}
          onManageAllowlist={() => props.imDetail!.onManageAllowlist(props.imDetail!.connection.connectionId)}
          onOpenSession={() => props.imDetail!.onOpenSession(props.imDetail!.connection)}
        />
      ) : noticePreviewMockEnabled() ? (
        <NoticePreviewPanel />
      ) : props.remote ? (
        <Suspense fallback={null}><RemoteSessionSurface tab={props.remote.tab} session={props.remote.session}
          surfaceCommitToken={transcript.surfaceCommitToken} onSurfacePaintReady={commands.onSurfacePaintReady} /></Suspense>
      ) : (
        <>
          <div className="transcript-navigation-surface" aria-busy={transitioning}>
            <div
              className="transcript-navigation-content"
              aria-hidden={transitioning || undefined}
              ref={(node) => {
                if (!node) return;
                (node as HTMLElement & { inert?: boolean }).inert = transitioning;
              }}
            >
              <Transcript
                items={transcript.items}
                live={transitioning ? undefined : state.live}
                liveStore={transcript.liveStore}
                tabId={transcript.tabId}
                geometrySessionKey={transcript.geometrySessionKey}
                footerHeight={transcript.footerHeight}
                onPrompt={commands.onPrompt}
                onDeliveryContinue={commands.onDeliveryContinue}
                onAcceptDelivery={commands.onAcceptDelivery}
                onOpenChanges={commands.onOpenChanges}
                onOpenVerification={commands.onOpenVerification}
                onEditPrompt={commands.onEditPrompt}
                onRewind={commands.onRewind}
                checkpoints={state.checkpoints}
                actionPending={state.messageAction != null}
                rewindDisabled={rewindDisabled}
                running={state.running || rewind.committing}
                turnStartAt={state.turnStartAt}
                contentRevision={state.historyLayoutRevision}
                historyMutation={state.historyMutation}
                welcomeVariant={transcript.creation ? "creation" : "default"}
                creationMode={transcript.creation}
                actionHoverMenus={transcript.creation && !transcript.hydratePlaceholderActive && !transitioning}
                rewindSignal={rewind.signal}
                revealSignal={transcript.revealSignal}
                hydrating={transcript.transcriptHydrating || (transitioning && !transcript.navigationDataReady)}
                hasOlderHistory={!transitioning && state.historyHasOlder && !rewind.stateActive}
                historyStartTurn={state.historyStartTurn}
                historyTotalTurns={state.historyTotalTurns}
                loadingOlderHistory={state.historyOlderLoading}
                olderHistoryError={state.historyOlderError}
                onLoadOlderHistory={commands.onLoadOlderHistory}
                invocationMetadata={transcript.invocationMetadata}
                surfaceCommitToken={transcript.surfaceCommitToken}
                onSurfacePaintReady={commands.onSurfacePaintReady}
              />
            </div>
            {transitioning ? (
              <div className="transcript-navigation-overlay" role="status" aria-live="polite">
                <span className="transcript-navigation-overlay__spinner" aria-hidden="true" />
                <span>{t("common.loading")}</span>
              </div>
            ) : null}
          </div>
          {!transitioning && state.hydrateError ? <div className="history-load-error" role="alert"><span>{state.hydrateError}</span><button type="button" className="btn btn--small" onClick={props.onRetryHistory}>{t("common.retry")}</button></div> : null}
        </>
      )}
    </main>
  );
}
