import type { RefObject } from "react";
import type { FlatIndexLocationWithAlign, StateSnapshot } from "react-virtuoso";
import type { TranscriptRecoveryCancelReason } from "./transcriptScrollArbiter";
import type { TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";

export type TranscriptRecoveryTerminal = {
  id: number;
  outcome: "done" | "cancelled" | "expired";
  reason?: TranscriptRecoveryCancelReason;
};

export type TranscriptRecoveryRequestSpec = {
  anchor: TranscriptLayoutAnchor;
  locate: (anchor: TranscriptLayoutAnchor) => FlatIndexLocationWithAlign | undefined;
  captureUserAnchor: () => TranscriptLayoutAnchor | undefined;
  onSettle?: (anchor: TranscriptLayoutAnchor) => void;
  onCancel?: (reason: TranscriptRecoveryCancelReason) => void;
  onSuspend?: (id: number) => void;
  onExpired?: (id: number) => void;
};

export type TranscriptScrollArbiterRecoveryApi = {
  submitRecoveryRequest: (spec: TranscriptRecoveryRequestSpec) => number;
  retryRecoveryRequest: (id: number) => void;
  lastGoodAnchorRef: RefObject<TranscriptLayoutAnchor | null>;
  captureStateSnapshot: () => StateSnapshot | null;
};

export type ActiveTranscriptRecovery = {
  id: number;
  spec: TranscriptRecoveryRequestSpec;
  anchor: TranscriptLayoutAnchor;
  retries: number;
  status: "active" | "suspended";
  stableFrames: number;
  deadline: number;
  frame: number | null;
};
