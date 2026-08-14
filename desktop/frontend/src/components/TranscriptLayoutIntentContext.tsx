import { createContext, useContext } from "react";

const TranscriptLayoutIntentContext = createContext<() => void>(() => {});

export const TranscriptLayoutIntentProvider = TranscriptLayoutIntentContext.Provider;

export function useTranscriptUserResizeIntent(): () => void {
  return useContext(TranscriptLayoutIntentContext);
}
