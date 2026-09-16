import type { HistoryMessage, TurnEventReplayView, TurnStatus, WireEvent, WireCompletionSummary } from "./types";
import type { TranscriptIdentity, TranscriptSnapshotBoundary } from "./turnEventProjection";
import type { HistorySwitchPhases } from "./sessionDiagnostics";

export interface TranscriptContentRef {
  snapshotId: string;
  recordId: string;
  path: string[];
  bytes: number;
}

export interface TranscriptRecord {
  id: string;
  order: number;
  message: HistoryMessage;
  refs: TranscriptContentRef[];
}

export interface TranscriptSnapshot extends TranscriptSnapshotBoundary {
  records: TranscriptRecord[];
  activeRecords: TranscriptRecord[];
  runtime: { turnId?: string; submissionId?: string; status?: TurnStatus; phase?: string; startedAt?: number; completionSummary?: WireCompletionSummary; pendingEvents: WireEvent[] };
  activeAttempts: Array<{ id: string; messageId: string }>;
  before: number;
  hasOlder: boolean;
  totalRecords: number;
	totalTurns: number;
  stale: boolean;
}

export interface TranscriptPageRequest {
  snapshotId?: string;
  before?: number;
  records?: number;
  bytes?: number;
}

/**
 * One user turn of the complete conversation. `id` is the stable record
 * identity shared with the body records; `order` is only this snapshot's
 * pagination position and must never be used as cross-snapshot identity or as
 * a React key.
 */
export interface TranscriptOutlineEntry {
  id: string;
  messageId?: string;
  turn: number;
  order: number;
  prompt: string;
  answer?: string;
}

export interface TranscriptOutlinePage {
  protocolVersion: number;
  snapshotId: string;
  entries: TranscriptOutlineEntry[];
  nextOffset: number;
  done: boolean;
  total: number;
  stale: boolean;
}

export interface TranscriptOutlineRequest { snapshotId: string; offset?: number; entries?: number; bytes?: number }

export interface TranscriptContentChunk { data: string; nextOffset: number; done: boolean; stale: boolean }
export interface TranscriptReplayRequest { identity: TranscriptIdentity; after: number }
export interface TranscriptReplay extends TranscriptSnapshotBoundary, TurnEventReplayView {}

export interface TranscriptProtocolBindings {
  TranscriptFollowForTab(tabId: string, request: import("../generated/desktopContract.generated").FollowRequest): Promise<import("../generated/desktopContract.generated").TranscriptFollowResponse>;
  RemoteTranscriptFollowForTab(tabId: string, request: import("../generated/desktopContract.generated").FollowRequest): Promise<import("../generated/desktopContract.generated").TranscriptFollowResponse>;
  TranscriptSnapshotForTab?(tabId: string, request: TranscriptPageRequest): Promise<TranscriptSnapshot>;
  TranscriptPageForTab?(tabId: string, request: TranscriptPageRequest): Promise<TranscriptSnapshot>;
  TranscriptContentForTab?(tabId: string, request: TranscriptContentRef & { offset: number }): Promise<TranscriptContentChunk>;
  /** Optional beside the body protocol: an absent method means no outline. */
  TranscriptOutlineForTab?(tabId: string, request: TranscriptOutlineRequest): Promise<TranscriptOutlinePage>;
  TranscriptReplayForTab?(tabId: string, request: TranscriptReplayRequest): Promise<TranscriptReplay>;
  RemoteTranscriptSnapshotForTab?(tabId: string, request: TranscriptPageRequest): Promise<{ supported: boolean; snapshot?: TranscriptSnapshot }>;
  RemoteTranscriptPageForTab?(tabId: string, request: TranscriptPageRequest): Promise<TranscriptSnapshot>;
  RemoteTranscriptContentForTab?(tabId: string, request: TranscriptContentRef & { offset: number }): Promise<TranscriptContentChunk>;
  RemoteTranscriptOutlineForTab?(tabId: string, request: TranscriptOutlineRequest): Promise<TranscriptOutlinePage>;
  RemoteTranscriptReplayForTab?(tabId: string, request: TranscriptReplayRequest): Promise<TranscriptReplay>;
  ResumeTranscriptSessionForTab?(tabID: string, path: string): Promise<HistorySwitchPhases | void>;
  OpenChannelTranscriptSessionForTab?(tabID: string, path: string): Promise<HistorySwitchPhases | void>;
}

export interface TranscriptTurnMetadata {
  samplingCount?: number;
  toolCount?: number;
  turnFinal?: boolean;
}
