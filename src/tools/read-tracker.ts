import * as pathMod from "node:path";

/** Tracks files the model has had byte-exact visibility into this session. `edit_file` and `multi_edit` consult it before mutating, so the SEARCH text matches the on-disk bytes the model actually saw — not what it guessed. */
export class ReadTracker {
  private readonly _seen = new Set<string>();

  private static norm(abs: string): string {
    const resolved = pathMod.resolve(abs);
    return process.platform === "win32" ? resolved.toLowerCase() : resolved;
  }

  markRead(abs: string): void {
    this._seen.add(ReadTracker.norm(abs));
  }

  hasRead(abs: string): boolean {
    return this._seen.has(ReadTracker.norm(abs));
  }

  reset(): void {
    this._seen.clear();
  }

  get size(): number {
    return this._seen.size;
  }
}
