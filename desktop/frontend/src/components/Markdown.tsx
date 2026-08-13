import { lazy, memo, startTransition, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

const MarkdownRenderer = lazy(() => import("./MarkdownRenderer"));
const MarkdownHistory = lazy(() => import("./MarkdownHistory"));
const STREAMING_TAIL_THRESHOLD = 8_000;
const FINALIZE_SETTLE_MS = 50;
const FINALIZE_IDLE_TIMEOUT_MS = 1_000;
const MARKDOWN_SECTION_TARGET_CHARS = 12_000;
const CROSS_SECTION_REFERENCE_RE = /(?:^ {0,3}\[[^\]\n]+\]:|\[\^[^\]\n]+\])/m;
const CROSS_SECTION_CONTAINER_RE = /^ {0,3}(?:>|(?:[-+*]|\d{1,9}[.)])(?:[ \t]+|$)|<)/;

function scanMarkdownSections(text: string): { boundaries: number[]; hasCrossSectionContainer: boolean } {
  const boundaries = [0];
  let lineStart = 0;
  let fence: { marker: string; length: number } | null = null;
  let displayMath = false;
  let boundaryAfterFence = false;
  let hasCrossSectionContainer = false;

  const addBoundary = (offset: number) => {
    if (offset > 0 && boundaries[boundaries.length - 1] !== offset) boundaries.push(offset);
  };

  while (lineStart < text.length) {
    const newline = text.indexOf("\n", lineStart);
    const lineEnd = newline === -1 ? text.length : newline + 1;
    const line = text.slice(lineStart, newline === -1 ? text.length : newline).replace(/\r$/, "");
    const trimmed = line.trim();

    if (fence) {
      const close = new RegExp(`^ {0,3}${fence.marker}{${fence.length},}[ \\t]*$`);
      if (close.test(line)) {
        fence = null;
        boundaryAfterFence = true;
      }
      lineStart = lineEnd;
      continue;
    }

    if (displayMath) {
      if (trimmed === "$$") {
        displayMath = false;
        boundaryAfterFence = true;
      }
      lineStart = lineEnd;
      continue;
    }

    if (boundaryAfterFence && trimmed !== "") {
      addBoundary(lineStart);
      boundaryAfterFence = false;
    }

    if (CROSS_SECTION_CONTAINER_RE.test(line)) {
      hasCrossSectionContainer = true;
    }
    const fenceMatch = /^ {0,3}(`{3,}|~{3,})/.exec(line);
    if (fenceMatch) {
      addBoundary(lineStart);
      fence = { marker: fenceMatch[1][0], length: fenceMatch[1].length };
    } else if (trimmed === "$$") {
      addBoundary(lineStart);
      displayMath = true;
    } else if (/^ {0,3}#{1,6}(?:[ \t]+|$)/.test(line)) {
      addBoundary(lineStart);
    }
    lineStart = lineEnd;
  }

  return { boundaries, hasCrossSectionContainer };
}

// Stable top-level sections let React.memo retain completed Markdown while the
// streaming tail changes. Cross-section references intentionally stay in one
// renderer because their definitions can affect nodes anywhere in the document.
export function splitStableMarkdownSections(text: string): string[] {
  if (text.length < MARKDOWN_SECTION_TARGET_CHARS || CROSS_SECTION_REFERENCE_RE.test(text)) return [text];
  const { boundaries, hasCrossSectionContainer } = scanMarkdownSections(text);
  if (hasCrossSectionContainer) return [text];
  if (boundaries.length === 1) return [text];

  const sections = boundaries.map((start, index) => text.slice(start, boundaries[index + 1] ?? text.length));
  const chunks: string[] = [];
  let current = "";
  for (const section of sections) {
    if (current && current.length + section.length > MARKDOWN_SECTION_TARGET_CHARS) {
      chunks.push(current);
      current = section;
    } else {
      current += section;
    }
  }
  if (current) chunks.push(current);
  return chunks.length > 1 ? chunks : [text];
}

type IdleWindow = Window & {
  requestIdleCallback?: (callback: () => void, options?: { timeout: number }) => number;
  cancelIdleCallback?: (handle: number) => void;
};

function scheduleMarkdownFinalization(callback: () => void): () => void {
  const idleWindow = window as IdleWindow;
  let cancelled = false;
  let idleHandle: number | null = null;
  let frameHandle: number | null = null;
  const timeoutHandle = window.setTimeout(() => {
    if (cancelled) return;
    const run = () => {
      if (cancelled) return;
      startTransition(callback);
    };
    if (idleWindow.requestIdleCallback) {
      idleHandle = idleWindow.requestIdleCallback(run, { timeout: FINALIZE_IDLE_TIMEOUT_MS });
    } else {
      frameHandle = requestAnimationFrame(run);
    }
  }, FINALIZE_SETTLE_MS);

  return () => {
    cancelled = true;
    window.clearTimeout(timeoutHandle);
    if (idleHandle !== null) idleWindow.cancelIdleCallback?.(idleHandle);
    if (frameHandle !== null) cancelAnimationFrame(frameHandle);
  };
}

export function streamingMarkdownCommitInterval(textLength: number): number {
  if (textLength >= 32_000) return 300;
  if (textLength >= 8_000) return 150;
  return 50;
}

const STREAMING_LIST_ITEM_RE = /^ {0,3}(?:[*+-]|\d{1,9}[.)])(?:[ \t]+|$)/;
const STREAMING_THEMATIC_BREAK_RE = /^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})[ \t]*$/;

function isStreamingListItemLine(line: string): boolean {
  return STREAMING_LIST_ITEM_RE.test(line) && !STREAMING_THEMATIC_BREAK_RE.test(line);
}

// Live parse prefix: last completed block. A later list marker commits prior
// items only — the new item stays in the tail so indented continuations can join.
export function streamingCommitTarget(text: string): string {
  let lineStart = 0;
  let fence: { marker: string; length: number } | null = null;
  let displayMath = false;
  let boundary = 0;
  while (lineStart < text.length) {
    const newline = text.indexOf("\n", lineStart);
    const lineEnd = newline === -1 ? text.length : newline + 1;
    const line = text.slice(lineStart, newline === -1 ? text.length : newline).replace(/\r$/, "");
    const terminated = newline !== -1;
    if (fence) {
      if (new RegExp(`^ {0,3}${fence.marker}{${fence.length},}[ \\t]*$`).test(line)) {
        fence = null;
        if (terminated) boundary = lineEnd;
      }
    } else if (displayMath) {
      if (line.trim() === "$$") {
        displayMath = false;
        if (terminated) boundary = lineEnd;
      }
    } else {
      const fenceMatch = /^ {0,3}(`{3,}|~{3,})/.exec(line);
      if (fenceMatch) fence = { marker: fenceMatch[1][0], length: fenceMatch[1].length };
      else if (line.trim() === "$$") displayMath = true;
      else if (terminated && line.trim() === "") boundary = lineEnd;
      // A heading interrupts a paragraph, so a partial heading line already
      // completes everything before it; a terminated one is itself complete.
      else if (/^ {0,3}#{1,6}[ \t]+/.test(line)) boundary = terminated ? lineEnd : lineStart;
      else if (isStreamingListItemLine(line)) boundary = lineStart;
    }
    lineStart = lineEnd;
  }
  return fence || displayMath ? text : text.slice(0, boundary);
}

export function useRenderedMarkdownText(text: string, streaming: boolean, holdIdleFinalization = false): string {
  const [renderedText, setRenderedText] = useState(text);
  const latestTextRef = useRef(text);
  const frameRef = useRef<number | null>(null);
  const timeoutRef = useRef<number | null>(null);
  const lastCommitAtRef = useRef(0);
  const wasStreamingRef = useRef(streaming);
  const finalizingTextRef = useRef<string | null>(null);
  const cancelFinalizationRef = useRef<(() => void) | null>(null);
  const finalizationStartedAtRef = useRef(0);
  const finalizationLengthRef = useRef(0);

  latestTextRef.current = text;

  useLayoutEffect(() => {
    const endedStreaming = wasStreamingRef.current && !streaming;
    wasStreamingRef.current = streaming;
    if (streaming) {
      cancelFinalizationRef.current?.();
      cancelFinalizationRef.current = null;
      finalizingTextRef.current = null;
      // A bounded live preview occasionally advances its window and drops an
      // old prefix. Discard the stale parsed tree before paint; the complete
      // replacement stays visible through StreamingMarkdownTail and is parsed
      // later under the normal adaptive budget.
      if (renderedText !== "" && !text.startsWith(renderedText)) {
        setRenderedText("");
      }
      return;
    }
    lastCommitAtRef.current = 0;
    if (frameRef.current !== null) {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
    if (timeoutRef.current !== null) {
      window.clearTimeout(timeoutRef.current);
      timeoutRef.current = null;
    }
    if (renderedText === text) {
      cancelFinalizationRef.current?.();
      cancelFinalizationRef.current = null;
      finalizingTextRef.current = null;
      if (finalizationStartedAtRef.current > 0) {
        performance.measure("reasonix:markdown-finalize", {
          start: finalizationStartedAtRef.current,
          end: performance.now(),
          detail: { textLength: finalizationLengthRef.current },
        });
        finalizationStartedAtRef.current = 0;
        finalizationLengthRef.current = 0;
      }
      return;
    }

    const canFinalizeWhenIdle =
      (endedStreaming || finalizingTextRef.current !== null) &&
      text.length >= STREAMING_TAIL_THRESHOLD &&
      text.startsWith(renderedText);
    if (canFinalizeWhenIdle) {
      // The worker path owns the final parse of a completed stream: holding
      // here keeps the committed prefix frozen instead of re-parsing the full
      // document on the main thread at idle time.
      if (holdIdleFinalization) return;
      if (finalizingTextRef.current === text) return;
      cancelFinalizationRef.current?.();
      finalizingTextRef.current = text;
      cancelFinalizationRef.current = scheduleMarkdownFinalization(() => {
        cancelFinalizationRef.current = null;
        finalizationStartedAtRef.current = performance.now();
        finalizationLengthRef.current = latestTextRef.current.length;
        setRenderedText(latestTextRef.current);
      });
      return;
    }

    cancelFinalizationRef.current?.();
    cancelFinalizationRef.current = null;
    finalizingTextRef.current = null;
    setRenderedText(text);
  }, [renderedText, streaming, text, holdIdleFinalization]);

  useLayoutEffect(() => {
    if (streaming) lastCommitAtRef.current = performance.now();
  }, [renderedText, streaming]);

  useEffect(() => {
    if (!streaming || frameRef.current !== null || timeoutRef.current !== null) return;
    if (streamingCommitTarget(text).length <= renderedText.length) return;
    const commit = () => {
      timeoutRef.current = null;
      frameRef.current = requestAnimationFrame(() => {
        frameRef.current = null;
        // Recompute at commit time: only ever advance to a newer boundary.
        const target = streamingCommitTarget(latestTextRef.current);
        setRenderedText((prev) => (target.length > prev.length ? target : prev));
      });
    };
    const now = performance.now();
    const elapsed = lastCommitAtRef.current === 0 ? Number.POSITIVE_INFINITY : now - lastCommitAtRef.current;
    const delay = streamingMarkdownCommitInterval(text.length) - elapsed;
    if (delay <= 0) commit();
    else timeoutRef.current = window.setTimeout(commit, delay);
  }, [renderedText, streaming, text]);

  useEffect(() => () => {
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
    if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current);
    cancelFinalizationRef.current?.();
  }, []);

  return renderedText;
}

const StreamingMarkdownTail = memo(function StreamingMarkdownTail({ text }: { text: string }) {
  const elementRef = useRef<HTMLDivElement>(null);
  const previousTextRef = useRef("");

  useLayoutEffect(() => {
    const element = elementRef.current;
    if (!element) return;
    const previousText = previousTextRef.current;
    const textNode = element.firstChild;
    if (text.startsWith(previousText) && textNode?.nodeType === Node.TEXT_NODE) {
      (textNode as Text).appendData(text.slice(previousText.length));
    } else {
      element.textContent = text;
    }
    previousTextRef.current = text;
  }, [text]);

  return <div ref={elementRef} className="md md--stream-tail" data-transcript-selection-source-fallback />;
});

export const Markdown = memo(function Markdown({
  text,
  plainStatusBlocks = false,
  streaming = false,
  entryId,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  streaming?: boolean;
  /** History entry id (`he:<entryId>` rows) — enables the parsed-block cache. */
  entryId?: string;
}) {
  // legacyMode: the worker/inline pipeline failed (Worker unavailable AND the
  // in-process parse threw) — fall back to the pre-Phase-E behavior:
  // the idle-time main-thread finalization below parses the full document.
  const [legacyMode, setLegacyMode] = useState(false);
  const handleWorkerError = useCallback(() => setLegacyMode(true), []);
  // While the worker owns the final parse of a completed stream, hold the
  // idle finalization so the full document is not ALSO parsed main-thread.
  const holdIdleFinalization = !streaming && !legacyMode;
  const renderedText = useRenderedMarkdownText(text, streaming, holdIdleFinalization);
  const sections = useMemo(() => splitStableMarkdownSections(renderedText), [renderedText]);
  const pendingText = (streaming || text.length >= STREAMING_TAIL_THRESHOLD) && text.startsWith(renderedText)
    ? text.slice(renderedText.length)
    : "";
  // A row that ever streamed keeps its already-parsed committed sections as
  // the parse-in-flight fallback; a fresh history mount shows the full text
  // as plain first (never truncated) until worker blocks swap in.
  const wasStreamingRef = useRef(false);
  if (streaming) wasStreamingRef.current = true;

  // Finalize timing parity with the old idle path: measure from stream
  // completion (or mount, for history) to the worker blocks swapping in.
  const finalizeStartRef = useRef(0);
  const finalizeLengthRef = useRef(0);
  useEffect(() => {
    if (streaming || legacyMode || finalizeStartRef.current > 0) return;
    if (text.length < STREAMING_TAIL_THRESHOLD) return;
    finalizeStartRef.current = performance.now();
    finalizeLengthRef.current = text.length;
  }, [streaming, legacyMode, text]);
  const handleWorkerParsed = useCallback(() => {
    if (finalizeStartRef.current === 0) return;
    performance.measure("reasonix:markdown-finalize", {
      start: finalizeStartRef.current,
      end: performance.now(),
      detail: { textLength: finalizeLengthRef.current },
    });
    finalizeStartRef.current = 0;
    finalizeLengthRef.current = 0;
  }, []);

  const committedView = (
    <>
      <Suspense fallback={<div className="md" data-transcript-selection-source-fallback>{renderedText}</div>}>
        {sections.length === 1 ? (
          <MarkdownRenderer text={renderedText} plainStatusBlocks={plainStatusBlocks} />
        ) : (
          <div className="md" data-markdown-sections={sections.length}>
            {sections.map((section, index) => (
              <MarkdownRenderer key={index} text={section} plainStatusBlocks={plainStatusBlocks} bare />
            ))}
          </div>
        )}
      </Suspense>
      {pendingText && <StreamingMarkdownTail text={pendingText} />}
    </>
  );

  if (streaming || legacyMode) return committedView;

  return (
    <Suspense fallback={<div className="md" data-transcript-selection-source-fallback>{text}</div>}>
      <MarkdownHistory
        text={text}
        plainStatusBlocks={plainStatusBlocks}
        entryId={entryId}
        fallback={wasStreamingRef.current
          ? committedView
          : <div className="md" data-transcript-selection-source-fallback>{text}</div>}
        onParsed={handleWorkerParsed}
        onError={handleWorkerError}
      />
    </Suspense>
  );
});
