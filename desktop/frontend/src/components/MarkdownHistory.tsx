import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { hastBlockToJsx } from "../lib/hastJsx";
import { estimateHastBytes, markdownContentRevision, type MarkdownBlock, type MarkdownParseResult } from "../lib/markdownPipeline";
import { getMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { getTranscriptStore } from "../lib/transcriptStore";
import { createComponents } from "./markdownComponents";
import { useChatFileCandidateReport } from "./ChatFileLinkContext";
import { MarkdownSourceTable } from "./MarkdownTable";
import "katex/dist/katex.min.css";
import "./harness-chat/MarkdownText.css";

const Block = memo(function Block({ block, components }: { block: MarkdownBlock; components: ReturnType<typeof createComponents> }) {
  return block.virtualTable ? <MarkdownSourceTable data={block.virtualTable} /> : hastBlockToJsx(block, components);
});

/** Worker parsing is independent of viewport geometry. Every block uses natural flow. */
const MarkdownHistory = memo(function MarkdownHistory({ text, streaming = false, plainStatusBlocks = false, cacheKey, fallback, onParsed, onError }: {
  text: string; streaming?: boolean; plainStatusBlocks?: boolean; cacheKey?: string; fallback: ReactNode;
  onParsed?: () => void; onError?: () => void;
}) {
  const revision = useMemo(() => markdownContentRevision(text), [text]);
  const [parsed, setParsed] = useState<{ text: string; result: MarkdownParseResult }>();
  const previous = useRef<MarkdownParseResult | undefined>(undefined);
  const root = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(typeof IntersectionObserver === "undefined");
  useEffect(() => {
    if (visible || typeof IntersectionObserver === "undefined" || !root.current) return;
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { setVisible(true); observer.disconnect(); }
    }, { rootMargin: "800px" });
    observer.observe(root.current);
    return () => observer.disconnect();
  }, [visible]);
  const components = useMemo(() => createComponents(plainStatusBlocks), [plainStatusBlocks]);
  // The cache is keyed by content revision (see TranscriptMarkdownCache.key), so
  // it answers whether or not this row carries a stable id — the cacheKey prop
  // stays for readability/debugging only (#10143).
  const cached = useMemo(() => getTranscriptStore().getMarkdown(cacheKey, revision), [cacheKey, revision]);
  useEffect(() => {
    if (!visible && !streaming) return;
    if (cached?.blocks) { onParsed?.(); return; }
    let cancelled = false;
    const request = getMarkdownWorkerClient().parse(text);
    void request.promise.then(result => {
      if (cancelled || !result) return;
      // Retain unchanged prefix AST identities across stream publications and
      // finalization. React keeps native selection and code disclosure hosts.
      const stable = previous.current?.blocks;
      if (stable) result.blocks = result.blocks.map((block, index) =>
        stable[index] && JSON.stringify(stable[index]) === JSON.stringify(block) ? stable[index] : block);
      previous.current = result;
      setParsed({ text, result });
      // Unconditional (except while streaming): the key is the content
      // revision, so it is always available. An identity-keyed cache had to skip
      // this write whenever the row had no stable id at hand, which is exactly
      // when the parse is most likely to be thrown away and repeated (#10143).
      if (!streaming) getTranscriptStore().setMarkdown(cacheKey, revision, {
        source: text, blocks: result.blocks, selectionText: result.selectionText,
        selectionRevision: result.selectionRevision,
        bytes: text.length * 2 + result.selectionText.length * 2 + estimateHastBytes(result.blocks),
      });
      onParsed?.();
    }).catch(() => { if (!cancelled) onError?.(); });
    return () => { cancelled = true; request.cancel(); };
  }, [cacheKey, cached, onError, onParsed, revision, streaming, text, visible]);
  const result = cached?.blocks ? cached : parsed && (parsed.text === text || text.startsWith(parsed.text)) ? parsed.result : undefined;
  // Only blocks the parser has already committed are reported, so a streaming
  // answer never asks the host about a half-written path.
  useChatFileCandidateReport(result?.blocks, revision);
  const nodes = useMemo(() => result?.blocks.map(block => <Block key={block.key} block={block} components={components} />), [result, components]);
  const pending = !cached?.blocks && parsed && text.startsWith(parsed.text) ? text.slice(parsed.text.length) : "";
  return <div ref={root} className="md" data-markdown-blocks={result?.blocks.length}>
    {result ? <>{nodes}{pending && <span style={{ whiteSpace: "pre-wrap" }}>{pending}</span>}</> : fallback}
  </div>;
});
export default MarkdownHistory;
