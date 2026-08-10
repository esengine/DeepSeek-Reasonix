import { measureElement as measureVirtualElement, type Virtualizer } from "@tanstack/react-virtual";
import type { TranscriptRow } from "./transcriptRows";

export interface TranscriptHeightCacheOptions {
  maxTabs?: number;
  maxRowsPerTab?: number;
}

export type TranscriptLayoutSnapshot = {
  signature: string;
  width: number;
};

export const EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT: TranscriptLayoutSnapshot = {
  signature: "w:0",
  width: 0,
};

type TabMeasurements = Map<string, number>;

export class TranscriptHeightCache {
  private readonly maxTabs: number;
  private readonly maxRowsPerTab: number;
  private readonly tabs = new Map<string, TabMeasurements>();

  constructor(options: TranscriptHeightCacheOptions = {}) {
    this.maxTabs = options.maxTabs ?? 3;
    this.maxRowsPerTab = options.maxRowsPerTab ?? 10_000;
  }

  private rowId(layoutSignature: string, rowKey: string): string {
    return `${layoutSignature}\u0000${rowKey}`;
  }

  private touchTab(tabId: string, create: boolean): TabMeasurements | undefined {
    const existing = this.tabs.get(tabId);
    if (existing) {
      this.tabs.delete(tabId);
      this.tabs.set(tabId, existing);
      return existing;
    }
    if (!create) return undefined;
    const rows = new Map<string, number>();
    this.tabs.set(tabId, rows);
    while (this.tabs.size > this.maxTabs) this.tabs.delete(this.tabs.keys().next().value!);
    return rows;
  }

  get(tabId: string, layoutSignature: string, rowKey: string): number | undefined {
    const rows = this.touchTab(tabId, false);
    const id = this.rowId(layoutSignature, rowKey);
    const value = rows?.get(id);
    if (value == null || !rows) return undefined;
    rows.delete(id);
    rows.set(id, value);
    return value;
  }

  set(tabId: string, layoutSignature: string, rowKey: string, height: number): void {
    if (!Number.isFinite(height) || height <= 0) return;
    const rows = this.touchTab(tabId, true)!;
    const id = this.rowId(layoutSignature, rowKey);
    rows.delete(id);
    rows.set(id, height);
    while (rows.size > this.maxRowsPerTab) rows.delete(rows.keys().next().value!);
  }
}

export const transcriptHeightCache = new TranscriptHeightCache();

export function createTranscriptMeasureElement({
  tabId,
  getLayoutSnapshot,
  cache = transcriptHeightCache,
}: {
  tabId: string;
  getLayoutSnapshot: () => TranscriptLayoutSnapshot;
  cache?: TranscriptHeightCache;
}) {
  return (
    element: HTMLDivElement,
    entry: ResizeObserverEntry | undefined,
    instance: Virtualizer<HTMLDivElement, HTMLDivElement>,
  ): number => {
    const height = measureVirtualElement(element, entry, instance);
    cache.set(
      tabId,
      getLayoutSnapshot().signature,
      element.dataset.rowKey ?? String(element.dataset.index ?? ""),
      height,
    );
    return height;
  };
}

export function readTranscriptLayoutSnapshot(element: HTMLElement | null): TranscriptLayoutSnapshot {
  if (!element || typeof getComputedStyle !== "function") return EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT;
  const style = getComputedStyle(element);
  const width = element.clientWidth;
  const widthBucket = Math.round(width / 32) * 32;
  return {
    signature: [
      `w:${widthBucket}`,
      `fs:${style.fontSize}`,
      `ff:${style.fontFamily}`,
      `lh:${style.lineHeight}`,
      `ls:${style.letterSpacing}`,
    ].join("|"),
    width: widthBucket,
  };
}

export function transcriptLayoutSignature(element: HTMLElement | null): string {
  return readTranscriptLayoutSnapshot(element).signature;
}

export function estimateTranscriptContentHeight(kind: "user" | "answer" | "extension", text: string, width: number): number {
  const charsPerLine = Math.max(32, Math.min(120, Math.floor((width - 64) / 8)));
  const lines = Math.max(1, Math.ceil(text.length / charsPerLine) + (text.match(/\n/g)?.length ?? 0));
  const raw = 48 + lines * 22;
  if (kind === "user") return Math.max(72, Math.min(360, raw));
  return Math.max(120, Math.min(1200, raw));
}

export function estimateCachedTranscriptRowHeight(row: TranscriptRow | undefined, width: number, fallback: number): number {
  if (!row) return fallback;
  if (row.kind === "user") return Math.max(fallback, estimateTranscriptContentHeight("user", row.item.text, width));
  if (row.kind === "answer") return Math.max(fallback, estimateTranscriptContentHeight("answer", row.item.text, width));
  if (row.kind === "reasoning") return Math.max(fallback, estimateTranscriptContentHeight("answer", row.item.reasoning, width));
  return fallback;
}

export function estimateTranscriptRowHeightForLayout({
  cache = transcriptHeightCache,
  tabId,
  layout,
  rowKey,
  row,
  fallback,
}: {
  cache?: TranscriptHeightCache;
  tabId: string;
  layout: TranscriptLayoutSnapshot;
  rowKey: string;
  row: TranscriptRow | undefined;
  fallback: number;
}): number {
  const cached = cache.get(tabId, layout.signature, rowKey);
  return cached ?? estimateCachedTranscriptRowHeight(row, layout.width, fallback);
}
