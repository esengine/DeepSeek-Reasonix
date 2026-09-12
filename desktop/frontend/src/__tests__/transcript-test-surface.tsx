import type { ComponentProps } from "react";
import { Transcript } from "../components/Transcript";
import type { TranscriptTestClock } from "./transcript-test-clock";

export function TranscriptTestSurface({
  viewportHeight,
  rowHeight,
  kernelClock,
  ...props
}: ComponentProps<typeof Transcript> & { viewportHeight: number; rowHeight: number; kernelClock?: TranscriptTestClock }) {
  void viewportHeight;
  void rowHeight;
  void kernelClock;
  return <Transcript {...props} />;
}
