import type { TranscriptKernel } from "./transcriptKernel";

/** Disconnect alone does not revoke already queued ResizeObserver deliveries. */
export function observeTranscriptGeometry(
  kernel: Pick<TranscriptKernel, "generation" | "afterCurrentGenerationPaint">,
  element: Element,
  before: () => unknown,
  commit: () => void,
  createObserver: (notify: () => void) => Pick<ResizeObserver, "observe" | "disconnect"> = (notify) => new ResizeObserver(notify),
): () => void {
  const generation = kernel.generation;
  let disposed = false;
  let cancelFrame: (() => void) | null = null;
  const current = () => !disposed && generation === kernel.generation;
  const observer = createObserver(() => {
    if (!current() || cancelFrame) return;
    before();
    cancelFrame = kernel.afterCurrentGenerationPaint(() => {
      cancelFrame = null;
      if (current()) commit();
    });
  });
  observer.observe(element);
  return () => {
    disposed = true;
    observer.disconnect();
    cancelFrame?.();
  };
}
