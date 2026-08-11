export type TranscriptSelectionPoint = {
  rowKey: string;
  /** UTF-16 offset in the row's readable text projection. */
  textOffset: number;
  affinity: "forward" | "backward";
};

export type TranscriptSelectionMode =
  | "none"
  | "native-dragging"
  | "native-settled"
  | "logical-dragging"
  | "logical-settled";

export type TranscriptSelectionSnapshot = {
  id: number;
  tabId: string;
  mode: TranscriptSelectionMode;
  anchor?: TranscriptSelectionPoint;
  focus?: TranscriptSelectionPoint;
  direction?: "forward" | "backward";
  contentRevisions: ReadonlyMap<string, number>;
};

export type TranscriptSelectableRow = {
  rowKey: string;
  /** Collapsed reasoning is absent from the row model; mounted state is irrelevant. */
  kind?: "message" | "reasoning";
  contentRevision: number;
  /** Frozen source; append-only changes keep the selected prefix valid. */
  sourceText: string;
  resolveText: () => Promise<string>;
  /** Pins an existing or future cached projection until selection cleanup. */
  pin?: () => () => void;
};

type FrozenSelection = {
  rows: readonly TranscriptSelectableRow[];
  rowIndex: ReadonlyMap<string, number>;
  releases: Array<{ rowIndex: number; release: () => void }>;
};

const EMPTY_REVISIONS: ReadonlyMap<string, number> = new Map();

function pointDirection(
  anchor: TranscriptSelectionPoint,
  focus: TranscriptSelectionPoint,
  rowIndex: ReadonlyMap<string, number>,
): "forward" | "backward" {
  const anchorIndex = rowIndex.get(anchor.rowKey) ?? 0;
  const focusIndex = rowIndex.get(focus.rowKey) ?? 0;
  if (anchorIndex !== focusIndex) return anchorIndex < focusIndex ? "forward" : "backward";
  return anchor.textOffset <= focus.textOffset ? "forward" : "backward";
}

function withAffinity(
  point: TranscriptSelectionPoint,
  affinity: TranscriptSelectionPoint["affinity"],
): TranscriptSelectionPoint {
  return point.affinity === affinity ? point : { ...point, affinity };
}

function samePoint(left: TranscriptSelectionPoint, right: TranscriptSelectionPoint): boolean {
  return left.rowKey === right.rowKey
    && left.textOffset === right.textOffset
    && left.affinity === right.affinity;
}

export class TranscriptSelectionStore {
  private listeners = new Set<() => void>();
  private nextId = 1;
  private frozen: FrozenSelection | null = null;
  private snapshot: TranscriptSelectionSnapshot = {
    id: 0,
    tabId: "",
    mode: "none",
    contentRevisions: EMPTY_REVISIONS,
  };

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): TranscriptSelectionSnapshot => this.snapshot;

  private publish(snapshot: TranscriptSelectionSnapshot): void {
    this.snapshot = snapshot;
    for (const listener of this.listeners) listener();
  }

  private releaseFrozen(): void {
    const frozen = this.frozen;
    this.frozen = null;
    if (!frozen) return;
    for (const pinned of frozen.releases) pinned.release();
  }

  beginNative(tabId: string): number {
    this.releaseFrozen();
    const id = this.nextId++;
    this.publish({ id, tabId, mode: "native-dragging", contentRevisions: EMPTY_REVISIONS });
    return id;
  }

  updateNativeRange(anchor: TranscriptSelectionPoint, focus: TranscriptSelectionPoint): void {
    if (this.snapshot.mode !== "native-dragging" && this.snapshot.mode !== "native-settled") return;
    const direction = anchor.rowKey === focus.rowKey && anchor.textOffset > focus.textOffset ? "backward" : "forward";
    this.publish({
      ...this.snapshot,
      anchor: withAffinity(anchor, direction),
      focus: withAffinity(focus, direction),
      direction,
    });
  }

  settleNative(): void {
    if (this.snapshot.mode !== "native-dragging") return;
    this.publish({ ...this.snapshot, mode: "native-settled" });
  }

  promoteToLogical(
    tabId: string,
    anchor: TranscriptSelectionPoint,
    focus: TranscriptSelectionPoint,
    rows: readonly TranscriptSelectableRow[],
  ): number | null {
    if (this.snapshot.tabId !== tabId || this.snapshot.mode !== "native-dragging") return null;
    const rowIndex = new Map(rows.map((row, index) => [row.rowKey, index]));
    if (!rowIndex.has(anchor.rowKey) || !rowIndex.has(focus.rowKey)) return null;
    this.releaseFrozen();
    const releases: FrozenSelection["releases"] = [];
    rows.forEach((row, index) => {
      if (!row.pin) return;
      try {
        releases.push({ rowIndex: index, release: row.pin() });
      } catch {
        // Cache pinning is an optimization; selection stays functional.
      }
    });
    this.frozen = { rows: [...rows], rowIndex, releases };
    const direction = pointDirection(anchor, focus, rowIndex);
    this.publish({
      id: this.snapshot.id,
      tabId,
      mode: "logical-dragging",
      anchor: withAffinity(anchor, direction),
      focus: withAffinity(focus, direction),
      direction,
      contentRevisions: new Map(rows.map((row) => [row.rowKey, row.contentRevision])),
    });
    return this.snapshot.id;
  }

  updateLogicalFocus(focus: TranscriptSelectionPoint): void {
    if (this.snapshot.mode !== "logical-dragging" || !this.snapshot.anchor || !this.frozen) return;
    if (!this.frozen.rowIndex.has(focus.rowKey)) return;
    const direction = pointDirection(this.snapshot.anchor, focus, this.frozen.rowIndex);
    const anchor = withAffinity(this.snapshot.anchor, direction);
    const nextFocus = withAffinity(focus, direction);
    if (
      this.snapshot.direction === direction
      && this.snapshot.focus
      && samePoint(this.snapshot.anchor, anchor)
      && samePoint(this.snapshot.focus, nextFocus)
    ) return;
    this.publish({
      ...this.snapshot,
      anchor,
      focus: nextFocus,
      direction,
    });
  }

  settleLogical(): void {
    const snapshot = this.snapshot;
    const frozen = this.frozen;
    if (snapshot.mode !== "logical-dragging" || !snapshot.anchor || !snapshot.focus || !frozen) return;
    const anchorIndex = frozen.rowIndex.get(snapshot.anchor.rowKey);
    const focusIndex = frozen.rowIndex.get(snapshot.focus.rowKey);
    if (anchorIndex == null || focusIndex == null) {
      this.clear("logical-endpoint-missing");
      return;
    }
    const low = Math.min(anchorIndex, focusIndex);
    const high = Math.max(anchorIndex, focusIndex);
    const rows = frozen.rows.slice(low, high + 1);
    const releases: FrozenSelection["releases"] = [];
    for (const pinned of frozen.releases) {
      if (pinned.rowIndex >= low && pinned.rowIndex <= high) {
        releases.push({ rowIndex: pinned.rowIndex - low, release: pinned.release });
      } else {
        pinned.release();
      }
    }
    this.frozen = {
      rows,
      rowIndex: new Map(rows.map((row, index) => [row.rowKey, index])),
      releases,
    };
    this.publish({
      ...snapshot,
      mode: "logical-settled",
      contentRevisions: new Map(rows.map((row) => [row.rowKey, row.contentRevision])),
    });
  }

  clear(_reason = "clear"): void {
    this.releaseFrozen();
    this.publish({
      id: this.nextId++,
      tabId: "",
      mode: "none",
      contentRevisions: EMPTY_REVISIONS,
    });
  }

  isCurrent(snapshotId: number, tabId?: string): boolean {
    return this.snapshot.id === snapshotId
      && this.snapshot.mode !== "none"
      && (tabId === undefined || this.snapshot.tabId === tabId);
  }

  isLogical(): boolean {
    return this.snapshot.mode === "logical-dragging" || this.snapshot.mode === "logical-settled";
  }

  isRowSelected(snapshotId: number, rowKey: string): boolean {
    return this.rowBounds(snapshotId, rowKey, Number.MAX_SAFE_INTEGER) !== null;
  }

  rowBounds(snapshotId: number, rowKey: string, textLength: number): { start: number; end: number } | null {
    const snapshot = this.snapshot;
    const frozen = this.frozen;
    if (snapshot.id !== snapshotId || !snapshot.anchor || !snapshot.focus || !frozen) return null;
    const anchorIndex = frozen.rowIndex.get(snapshot.anchor.rowKey);
    const focusIndex = frozen.rowIndex.get(snapshot.focus.rowKey);
    const rowIndex = frozen.rowIndex.get(rowKey);
    if (anchorIndex == null || focusIndex == null || rowIndex == null) return null;
    const lowIndex = Math.min(anchorIndex, focusIndex);
    const highIndex = Math.max(anchorIndex, focusIndex);
    if (rowIndex < lowIndex || rowIndex > highIndex) return null;
    const clamp = (value: number) => Math.max(0, Math.min(textLength, value));
    if (anchorIndex === focusIndex) {
      return {
        start: clamp(Math.min(snapshot.anchor.textOffset, snapshot.focus.textOffset)),
        end: clamp(Math.max(snapshot.anchor.textOffset, snapshot.focus.textOffset)),
      };
    }
    const lowPoint = anchorIndex < focusIndex ? snapshot.anchor : snapshot.focus;
    const highPoint = anchorIndex < focusIndex ? snapshot.focus : snapshot.anchor;
    return {
      start: rowIndex === lowIndex ? clamp(lowPoint.textOffset) : 0,
      end: rowIndex === highIndex ? clamp(highPoint.textOffset) : textLength,
    };
  }

  validateRows(rows: readonly TranscriptSelectableRow[]): boolean {
    const snapshot = this.snapshot;
    const frozen = this.frozen;
    if (!frozen || !snapshot.anchor || !snapshot.focus || !this.isLogical()) return true;
    const current = new Map(rows.map((row) => [row.rowKey, row]));
    const anchorIndex = frozen.rowIndex.get(snapshot.anchor.rowKey);
    const focusIndex = frozen.rowIndex.get(snapshot.focus.rowKey);
    if (anchorIndex == null || focusIndex == null) return false;
    const low = Math.min(anchorIndex, focusIndex);
    const high = Math.max(anchorIndex, focusIndex);
    for (let index = low; index <= high; index += 1) {
      const previous = frozen.rows[index];
      const next = current.get(previous.rowKey);
      if (!next || (next.contentRevision !== previous.contentRevision && !next.sourceText.startsWith(previous.sourceText))) {
        this.clear("selected-row-changed");
        return false;
      }
    }
    return true;
  }

  /** Validate a small set of live rows without rebuilding a map of all history. */
  validateRowChanges(rows: readonly TranscriptSelectableRow[]): boolean {
    const snapshot = this.snapshot;
    const frozen = this.frozen;
    if (!frozen || !snapshot.anchor || !snapshot.focus || !this.isLogical()) return true;
    const anchorIndex = frozen.rowIndex.get(snapshot.anchor.rowKey);
    const focusIndex = frozen.rowIndex.get(snapshot.focus.rowKey);
    if (anchorIndex == null || focusIndex == null) return false;
    const low = Math.min(anchorIndex, focusIndex);
    const high = Math.max(anchorIndex, focusIndex);
    for (const next of rows) {
      const index = frozen.rowIndex.get(next.rowKey);
      if (index == null || index < low || index > high) continue;
      const previous = frozen.rows[index];
      if (next.contentRevision !== previous.contentRevision && !next.sourceText.startsWith(previous.sourceText)) {
        this.clear("selected-row-changed");
        return false;
      }
    }
    return true;
  }

  async resolveText(snapshotId: number): Promise<string> {
    const snapshot = this.snapshot;
    const frozen = this.frozen;
    if (snapshot.id !== snapshotId || !snapshot.anchor || !snapshot.focus || !frozen) return "";
    const anchorIndex = frozen.rowIndex.get(snapshot.anchor.rowKey);
    const focusIndex = frozen.rowIndex.get(snapshot.focus.rowKey);
    if (anchorIndex == null || focusIndex == null) return "";
    const low = Math.min(anchorIndex, focusIndex);
    const high = Math.max(anchorIndex, focusIndex);
    const values: string[] = [];
    for (let index = low; index <= high; index += 1) {
      const row = frozen.rows[index];
      let text = row.sourceText;
      try {
        text = await row.resolveText();
      } catch {
        // Raw Markdown/source text is the final non-empty fallback.
      }
      const bounds = this.rowBounds(snapshotId, row.rowKey, text.length);
      if (!bounds) return "";
      values.push(text.slice(bounds.start, bounds.end));
    }
    return values.join("\n\n");
  }
}

export const transcriptSelectionStore = new TranscriptSelectionStore();
