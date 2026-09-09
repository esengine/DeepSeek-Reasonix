import type { ComponentProps } from "react";
import { Transcript } from "../components/Transcript";
import { TranscriptKernelClockContext } from "../lib/useTranscriptKernel";
import type { TranscriptKernelClock } from "../lib/transcriptKernel";

export function TranscriptTestSurface({
  viewportHeight,
  rowHeight,
  kernelClock,
  ...props
}: ComponentProps<typeof Transcript> & { viewportHeight: number; rowHeight: number; kernelClock?: TranscriptKernelClock }) {
  void viewportHeight;
  void rowHeight;
  return <TranscriptKernelClockContext.Provider value={kernelClock}><Transcript {...props} /></TranscriptKernelClockContext.Provider>;
}
