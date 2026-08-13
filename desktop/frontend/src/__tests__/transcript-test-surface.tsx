import type { ComponentProps } from "react";
import { VirtuosoMockContext } from "react-virtuoso";
import { Transcript } from "../components/Transcript";

export function TranscriptTestSurface({
  viewportHeight,
  rowHeight,
  ...props
}: ComponentProps<typeof Transcript> & { viewportHeight: number; rowHeight: number }) {
  return (
    <VirtuosoMockContext.Provider value={{ viewportHeight, itemHeight: rowHeight }}>
      <Transcript {...props} />
    </VirtuosoMockContext.Provider>
  );
}
