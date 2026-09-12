import { lazy, memo, startTransition, Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { t } from "../lib/i18n";

async function loadMarkdownView<T>(component: Promise<T>): Promise<T> {
  await import("./MarkdownImage.css");
  return component;
}

let historyView: typeof import("./MarkdownHistory").default | undefined;
let historyViewPromise: Promise<typeof import("./MarkdownHistory")> | undefined;
export function preloadMarkdownHistory(): Promise<typeof import("./MarkdownHistory")> {
  return historyViewPromise ??= loadMarkdownView(import("./MarkdownHistory")).then(module => {
    historyView = module.default;
    return module;
  });
}
const LazyMarkdownHistory = lazy(preloadMarkdownHistory);
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
// An open code fence commits only up to the fence line: the tail renders the
// growing code with code styling (splitStreamingTailFence), so streaming a
// large block no longer re-parses the whole document on every commit.
export function streamingCommitTarget(text: string): string {
  let lineStart = 0;
  let fence: { marker: string; length: number } | null = null;
  let fenceStart = 0;
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
      if (fenceMatch) {
        fence = { marker: fenceMatch[1][0], length: fenceMatch[1].length };
        fenceStart = lineStart;
      } else if (line.trim() === "$$") displayMath = true;
      else if (terminated && line.trim() === "") boundary = lineEnd;
      // A heading interrupts a paragraph, so a partial heading line already
      // completes everything before it; a terminated one is itself complete.
      else if (/^ {0,3}#{1,6}[ \t]+/.test(line)) boundary = terminated ? lineEnd : lineStart;
      else if (isStreamingListItemLine(line)) boundary = lineStart;
    }
    lineStart = lineEnd;
  }
  return fence ? text.slice(0, fenceStart) : displayMath ? text : text.slice(0, boundary);
}

type StreamingTailFence = { head: string; lang: string; code: string };

// Split a streaming tail around an unclosed code fence so the fence body can
// render with code styling before the closing fence arrives. Bail out cheaply
// when no fence marker exists; otherwise mirror the fence state machine from
// streamingCommitTarget in one forward pass over the tail.
export function splitStreamingTailFence(text: string): StreamingTailFence | null {
  if (!text.includes("```") && !text.includes("~~~")) return null;
  let lineStart = 0;
  let fence: { marker: string; length: number } | null = null;
  let fenceStart = 0;
  let fenceBodyStart = 0;
  let lang = "";
  while (lineStart < text.length) {
    const newline = text.indexOf("\n", lineStart);
    const lineEnd = newline === -1 ? text.length : newline + 1;
    const line = text.slice(lineStart, newline === -1 ? text.length : newline).replace(/\r$/, "");
    if (fence) {
      if (new RegExp(`^ {0,3}${fence.marker}{${fence.length},}[ \\t]*$`).test(line)) fence = null;
    } else {
      const fenceMatch = /^ {0,3}(`{3,}|~{3,})([^\n]*)$/.exec(line);
      if (fenceMatch) {
        fence = { marker: fenceMatch[1][0], length: fenceMatch[1].length };
        fenceStart = lineStart;
        fenceBodyStart = lineEnd;
        lang = fenceMatch[2].trim();
      }
    }
    lineStart = lineEnd;
  }
  if (!fence) return null;
  return { head: text.slice(0, fenceStart), lang, code: text.slice(fenceBodyStart) };
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

export const Markdown = memo(function Markdown({
  text, plainStatusBlocks = false, streaming = false, cacheKey,
}: { text: string; plainStatusBlocks?: boolean; streaming?: boolean; cacheKey?: string; wasStreamed?: boolean }) {
  const renderedText = useRenderedMarkdownText(text, streaming, false);
  const [failed, setFailed] = useState<string>();
  const onError = useCallback(() => setFailed(text), [text]);
  const History = historyView ?? LazyMarkdownHistory;
  const fallback = <div className="md" style={{ whiteSpace: "pre-wrap" }}>{text}</div>;
  if (failed === text) return <><span className="chat-notice" role="status">{t("chat.parseFailed")}</span>{fallback}</>;
  return <Suspense fallback={fallback}>
    <History text={streaming ? renderedText : text} streaming={streaming}
      plainStatusBlocks={plainStatusBlocks} cacheKey={cacheKey}
      fallback={<span style={{ whiteSpace: "pre-wrap" }}>{streaming ? renderedText : text}</span>} onError={onError} />
    {streaming && text.startsWith(renderedText) && text.length > renderedText.length &&
      <span className="md" style={{ whiteSpace: "pre-wrap" }}>{text.slice(renderedText.length)}</span>}
  </Suspense>;
});
