import { createContext, useContext } from "react";

/** Automatic content replacement and its natural-size publication share the
 * window's input owner and before-paint measurement boundary. */
const TranscriptPresentationContext = createContext({
  gestureActive: false,
  windowed: false,
  geometryChanged: () => {},
});
export const TranscriptPresentationProvider = TranscriptPresentationContext.Provider;
export const useTranscriptPresentation = () => useContext(TranscriptPresentationContext);
