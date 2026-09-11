import { useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { activeTabMirror } from "./activeTabMirror";
import type { InvocationMetadataMap } from "../lib/invocationDisplay";

/**
 * Owns the per-tab invocation metadata ledger: commits are bound to the
 * layout-committed active tab through the mirror, never a stale render
 * capture, and identical kind/color maps commit as no-ops.
 */
export function useInvocationMetadata() {
  const [invocationMetadataByTab, setInvocationMetadataByTab] = useState<Record<string, InvocationMetadataMap>>({});
  const handleInvocationMetadataChange = useCommittedCommand((metadata: InvocationMetadataMap) => {
    const sourceTabId = activeTabMirror().current;
    if (!sourceTabId) return;
    setInvocationMetadataByTab((current) => {
      const previous = current[sourceTabId] ?? {};
      const names = Object.keys(metadata);
      if (names.length === Object.keys(previous).length && names.every((name) => (
        previous[name]?.kind === metadata[name]?.kind && previous[name]?.color === metadata[name]?.color
      ))) return current;
      return { ...current, [sourceTabId]: metadata };
    });
  });
  return { invocationMetadataByTab, handleInvocationMetadataChange };
}
