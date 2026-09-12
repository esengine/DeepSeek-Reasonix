import { app } from "./bridge";
import { historyEntryIdForItemId } from "./transcriptHistoryEntry";
import { getTranscriptStore } from "./transcriptStore";
import type { Item } from "./useController";

/** Four requests per mounted session, with deduplication and disposal fencing. */
export class ChatContentLoader {
  private active = 0;
  private closed = false;
  private generation = 0;
  private queue: Array<() => void> = [];
  private pending = new Map<string, { item: Item; promise: Promise<string> }>();
  constructor(private tabId?: string, private resolve?: (item: Item, field: "content" | "reasoning" | "tool") => Promise<string>) {}
  activate() { this.closed = false; }
  needsFullContent(item: Item, field: "reasoning" | "tool"): boolean {
    if (field === "tool" && item.kind === "tool" && (item.dataArchived || item.truncated)) return true;
    const entry = historyEntryIdForItemId(item.id);
    return Boolean(entry && this.tabId && getTranscriptStore().hasContentReference(this.tabId, entry, field));
  }
  load = (item: Item, field: "content" | "reasoning" | "tool"): Promise<string> => {
    const key = `${item.id}:${field}`;
    const previous = this.pending.get(key);
    if (previous && sameContent(previous.item, item)) return previous.promise;
    const generation = this.generation;
    const request = new Promise<string>((resolve, reject) => {
      const run = () => {
        if (this.closed || generation !== this.generation) { reject(new Error("Content view closed")); return; }
        this.active++;
        void (this.resolve ? this.resolve(item, field) : this.fetch(item, field)).then(value => {
          if (this.closed || generation !== this.generation) reject(new Error("Content view closed")); else resolve(value);
        }, reject).finally(() => {
          this.active--;
          if (this.pending.get(key)?.promise === request) this.pending.delete(key);
          this.queue.shift()?.();
        });
      };
      if (this.active < 4) run(); else this.queue.push(run);
    });
    this.pending.set(key, { item, promise: request });
    return request;
  };
  private async fetch(item: Item, field: "content" | "reasoning" | "tool"): Promise<string> {
    if (field === "tool" && item.kind === "tool") {
      if (this.tabId && getTranscriptStore().hasContentResolver(this.tabId)) {
        const full = await getTranscriptStore().requestFullContent(this.tabId, item.id, "tool");
        if (full === undefined) throw new Error("Tool content unavailable");
        return full;
      }
      let value: Record<string, unknown> = { args: item.args, output: item.output, error: item.error, diff: item.fileDiff };
      if (item.dataArchived) {
        if (!this.tabId) throw new Error("Tool content unavailable");
        const archived = await app.ToolResultForTab(this.tabId, item.id);
        if (!archived) throw new Error("Tool content unavailable");
        value = { ...value, ...archived };
      }
      return (this.tabId && await getTranscriptStore().requestToolContent(this.tabId, item, value)) || JSON.stringify(value, null, 2);
    }
    const fallback = item.kind === "assistant" ? field === "reasoning" ? item.reasoning : item.text
      : item.kind === "user" || item.kind === "phase" || item.kind === "notice" ? item.text : "";
    const entry = item.kind === "user" && item.messageId ? `m:${item.messageId}` : historyEntryIdForItemId(item.id);
    if (!entry || !this.tabId) return fallback;
    const store = getTranscriptStore();
    const text = await store.requestFullContent(this.tabId, entry, field);
    if (text === undefined && store.hasContentReference(this.tabId, entry, field)) throw new Error("Content reference unavailable; retry");
    return text ?? fallback;
  }
  dispose() { this.generation++; this.closed = true; const waiting = this.queue; this.queue = []; waiting.forEach(run => run()); this.pending.clear(); }
}

function sameContent(a: Item, b: Item): boolean {
  if (a === b) return true;
  if (a.kind === "assistant" && b.kind === "assistant") return a.text === b.text && a.reasoning === b.reasoning && a.streaming === b.streaming;
  if (a.kind === "tool" && b.kind === "tool") return a.args === b.args && a.output === b.output && a.error === b.error && a.fileDiff === b.fileDiff && a.dataArchived === b.dataArchived;
  return "text" in a && "text" in b && a.kind === b.kind && a.text === b.text;
}
