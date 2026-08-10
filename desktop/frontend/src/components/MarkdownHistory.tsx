// MarkdownHistory — history (non-streaming) Markdown rendering driven by the
// parse worker (Phase E). Mounted rows: check the transcript markdown cache
// (entryId + content revision) → on miss request a worker parse → render the
// resulting HAST blocks with the same components map react-markdown uses.
// Unmounted rows never reach this component, so cold-zone rows never parse.
//
// While a parse is in flight the caller-provided fallback stays on screen
// (plain full text for a fresh history mount — never truncated — or the
// committed streaming view when a live answer just completed). A single huge
// row mounts its blocks progressively: the first chunk immediately, the rest
// in idle slices so one 500KiB answer cannot monopolize a frame.

import { Fragment, memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { hastBlockToJsx } from "../lib/hastJsx";
import { scheduleIdleTask } from "../lib/idleTask";
import {
  estimateHastBytes,
  markdownContentRevision,
  type MarkdownBlock,
} from "../lib/markdownPipeline";
import { getMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { getTranscriptStore } from "../lib/transcriptStore";
import { createComponents } from "./markdownComponents";
import { VirtualMarkdownSourceTable } from "./MarkdownTable";

// Blocks beyond this count mount in idle chunks instead of one commit.
const PROGRESSIVE_INITIAL_BLOCKS = 24;
const PROGRESSIVE_CHUNK_BLOCKS = 24;

function cachedBlocks(entryId: string | undefined, revision: number, text: string): MarkdownBlock[] | undefined {
  if (!entryId) return undefined;
  const cached = getTranscriptStore().getMarkdown(entryId, revision);
  // The revision is a content hash; the stored source comparison is the
  // fidelity backstop against collisions and stale writes.
  return cached && cached.source === text ? cached.blocks : undefined;
}

/** Mount blocks progressively once a document exceeds the initial chunk. */
function useProgressiveBlockCount(total: number): number {
  const immediate = total <= PROGRESSIVE_INITIAL_BLOCKS;
  const [count, setCount] = useState(immediate ? total : PROGRESSIVE_INITIAL_BLOCKS);
  const [prevTotal, setPrevTotal] = useState(total);
  if (prevTotal !== total) {
    // New document (text/revision changed): restart from the initial chunk.
    setPrevTotal(total);
    setCount(total <= PROGRESSIVE_INITIAL_BLOCKS ? total : PROGRESSIVE_INITIAL_BLOCKS);
  }
  useEffect(() => {
    if (count >= total) return;
    // Each completed chunk re-runs this effect and schedules the next slice.
    return scheduleIdleTask(() => {
      setCount((current) => Math.min(total, current + PROGRESSIVE_CHUNK_BLOCKS));
    });
  }, [count, total]);
  return Math.min(count, total);
}

export const MarkdownHistory = memo(function MarkdownHistory({
  text,
  plainStatusBlocks = false,
  entryId,
  fallback,
  onParsed,
  onError,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  /** History entry id (`he:<entryId>` rows) — enables the parsed-block cache. */
  entryId?: string;
  /** What to show while the worker parses (plain text or the streaming view). */
  fallback: ReactNode;
  onParsed?: () => void;
  onError?: () => void;
}) {
  const revision = useMemo(() => markdownContentRevision(text), [text]);
  // Parsed state is keyed by its source text: a text change renders the
  // fallback (never stale blocks) until the new parse lands.
  const [parsed, setParsed] = useState<{ text: string; blocks: MarkdownBlock[] } | undefined>(() => {
    const cached = cachedBlocks(entryId, revision, text);
    return cached ? { text, blocks: cached } : undefined;
  });
  const blocks = parsed && parsed.text === text ? parsed.blocks : undefined;

  useEffect(() => {
    const cached = cachedBlocks(entryId, revision, text);
    if (cached) {
      setParsed({ text, blocks: cached });
      onParsed?.();
      return;
    }
    const handle = getMarkdownWorkerClient().parse(text);
    let cancelled = false;
    handle.promise
      .then((parsedBlocks) => {
        if (cancelled || !parsedBlocks) return;
        if (entryId) {
          getTranscriptStore().setMarkdown(entryId, revision, {
            source: text,
            blocks: parsedBlocks,
            bytes: text.length * 2 + estimateHastBytes(parsedBlocks),
          });
        }
        setParsed({ text, blocks: parsedBlocks });
        onParsed?.();
      })
      .catch(() => {
        if (!cancelled) onError?.();
      });
    return () => {
      cancelled = true;
      handle.cancel();
    };
    // onParsed/onError are stable caller callbacks; re-running per identity
    // change would re-request parses the cache already serves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text, entryId, revision]);

  const components = useMemo(() => createComponents(plainStatusBlocks), [plainStatusBlocks]);
  const visibleCount = useProgressiveBlockCount(blocks?.length ?? 0);

  // JSX per block depends only on the block and the components map; build it
  // lazily so progressive mounting never re-converts settled blocks.
  const jsxCacheRef = useRef<{ blocks: MarkdownBlock[]; nodes: ReactNode[] } | null>(null);
  if (!blocks) return <>{fallback}</>;
  let cache = jsxCacheRef.current;
  if (!cache || cache.blocks !== blocks) {
    cache = { blocks, nodes: new Array<ReactNode>(blocks.length) };
    jsxCacheRef.current = cache;
  }
  return (
    <div
      className="md"
      data-markdown-blocks={blocks.length}
      data-markdown-visible-blocks={visibleCount}
    >
      {blocks.slice(0, visibleCount).map((block, index) => {
        const cached = cache.nodes[index];
        if (cached !== undefined) return <Fragment key={block.key}>{cached}</Fragment>;
        const node = block.virtualTable
          ? <VirtualMarkdownSourceTable data={block.virtualTable} />
          : hastBlockToJsx(block, components);
        cache.nodes[index] = node;
        return <Fragment key={block.key}>{node}</Fragment>;
      })}
    </div>
  );
});

export default MarkdownHistory;
