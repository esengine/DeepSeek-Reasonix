export type TranscriptMeasurementChange = {
  key: string;
  size: number;
};

/**
 * Immutable, block-keyed DOM measurement snapshots for the Transcript window.
 * A render can observe either the old snapshot or the complete new snapshot,
 * never the partially-updated prefix tree produced by per-item publication.
 */
export class TranscriptMeasurementLedger {
  private sizes: ReadonlyMap<string, number> = new Map();
  private staged = new Map<string, number>();
  sizeFor(key: string, fallback: number): number {
    return this.sizes.get(key) ?? fallback;
  }

  commit(changes: readonly TranscriptMeasurementChange[]): boolean {
    if (changes.length === 0) return false;
    const next = new Map(this.sizes);
    let changed = false;
    for (const change of changes) {
      if (!change.key || !Number.isFinite(change.size) || change.size <= 0) continue;
      if (Math.abs((next.get(change.key) ?? 0) - change.size) <= 0.5) {
        this.staged.delete(change.key);
        continue;
      }
      next.set(change.key, change.size);
      this.staged.delete(change.key);
      changed = true;
    }
    if (!changed) return false;
    this.sizes = next;
    return true;
  }

  stage(changes: readonly TranscriptMeasurementChange[]): boolean {
    let changed = false;
    for (const change of changes) {
      if (!change.key || !Number.isFinite(change.size) || change.size <= 0) continue;
      const current = this.staged.get(change.key) ?? this.sizes.get(change.key) ?? 0;
      if (Math.abs(current - change.size) <= 0.5) continue;
      this.staged.set(change.key, change.size);
      changed = true;
    }
    return changed;
  }

  publishStaged(canPublish: (key: string) => boolean = () => true): readonly TranscriptMeasurementChange[] {
    const publishable: TranscriptMeasurementChange[] = [];
    for (const [key, size] of this.staged) {
      if (canPublish(key)) publishable.push({ key, size });
    }
    if (!this.commit(publishable)) return [];
    return publishable;
  }

  retain(keys: ReadonlySet<string>): boolean {
    for (const key of this.staged.keys()) {
      if (!keys.has(key)) this.staged.delete(key);
    }
    if (this.sizes.size === 0) return false;
    const next = new Map<string, number>();
    for (const [key, size] of this.sizes) {
      if (keys.has(key)) next.set(key, size);
    }
    if (next.size === this.sizes.size) return false;
    this.sizes = next;
    return true;
  }
}
