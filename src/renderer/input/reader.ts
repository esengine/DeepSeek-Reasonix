import type { Keystroke } from "./keystroke.js";
import { parseKeystrokes } from "./keystroke.js";

export type KeystrokeListener = (key: Keystroke) => void;

export interface KeystrokeSource {
  on(event: "data", cb: (chunk: Buffer | string) => void): void;
  off(event: "data", cb: (chunk: Buffer | string) => void): void;
  setRawMode?(raw: boolean): void;
  resume?(): void;
  pause?(): void;
}

export interface KeystrokeReaderOptions {
  source: KeystrokeSource;
  rawMode?: boolean;
}

export class KeystrokeReader {
  private listeners: KeystrokeListener[] = [];
  private readonly onData: (chunk: Buffer | string) => void;
  private destroyed = false;

  constructor(private readonly opts: KeystrokeReaderOptions) {
    this.onData = (chunk) => this.handle(chunk);
    if (opts.rawMode !== false) {
      opts.source.setRawMode?.(true);
    }
    opts.source.resume?.();
    opts.source.on("data", this.onData);
  }

  subscribe(listener: KeystrokeListener): () => void {
    this.listeners.push(listener);
    return () => {
      const i = this.listeners.indexOf(listener);
      if (i >= 0) this.listeners.splice(i, 1);
    };
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.opts.source.off("data", this.onData);
    if (this.opts.rawMode !== false) {
      this.opts.source.setRawMode?.(false);
    }
    this.listeners.length = 0;
  }

  private handle(chunk: Buffer | string): void {
    if (this.destroyed) return;
    const text = typeof chunk === "string" ? chunk : chunk.toString("utf8");
    const keys = parseKeystrokes(text);
    for (const key of keys) {
      for (const cb of this.listeners) cb(key);
    }
  }
}
