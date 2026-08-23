import { createContext, useContext } from "react";
import type { TranscriptScrollOwner } from "../lib/transcriptScrollArbiter";

const TranscriptLayoutIntentContext = createContext<() => void>(() => {});

export const TranscriptLayoutIntentProvider = TranscriptLayoutIntentContext.Provider;

export function useTranscriptUserResizeIntent(): () => void {
  return useContext(TranscriptLayoutIntentContext);
}

// Rows deep in the transcript tree (e.g. MarkdownHistory's block window)
// reach the scroll arbiter's offset-write channel through this context. The
// single-writer rule (desktop/AGENTS.md) forbids raw scroller writes, so
// without a provider there is nobody to route to and compensation is skipped.
type TranscriptScrollOffsetWrite = (owner: TranscriptScrollOwner, top: number, behavior?: ScrollBehavior) => boolean;

const TranscriptScrollWriteContext = createContext<TranscriptScrollOffsetWrite | null>(null);

export const TranscriptScrollWriteProvider = TranscriptScrollWriteContext.Provider;

export function useTranscriptScrollOffsetWrite(): TranscriptScrollOffsetWrite | null {
  return useContext(TranscriptScrollWriteContext);
}
