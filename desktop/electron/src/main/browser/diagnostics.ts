export interface PageDiagnostic { sequence: number; time: number; kind: "console" | "exception" | "network" | "navigation"; message: string; url?: string; status?: number }

export function diagnosticURL(value: string): string {
  try { const url = new URL(value); if (url.protocol !== "http:" && url.protocol !== "https:") return ""; url.username = ""; url.password = ""; url.search = ""; url.hash = ""; return url.href.slice(0, 1024); }
  catch { return ""; }
}
export function diagnosticText(value: string): string {
  return value.slice(0, 4096)
    .replace(/https?:\/\/[^\s"'<>]+/gi, diagnosticURL)
    .replace(/((?:authorization|set-cookie|cookie)["']?\s*[:=]\s*)[^\r\n]*/gi, "$1[redacted]")
    .replace(/(password|secret|token|api[-_]?key)["']?\s*[:=]\s*(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;]+)/gi, "$1=[redacted]")
    .replace(/Bearer\s+[^\s,;]+/gi, "Bearer [redacted]");
}

export class DiagnosticBuffer {
  private entries: PageDiagnostic[] = [];
  private sequence = 0;
  private bytes = 0;
  private readonly listeners = new Set<(entry: PageDiagnostic) => void>();
  subscribe(listener: (entry: PageDiagnostic) => void): () => void { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; }
  add(entry: Omit<PageDiagnostic, "sequence" | "time">): void {
    const item = { ...entry, sequence: ++this.sequence, time: Date.now(), message: diagnosticText(entry.message), url: entry.url ? diagnosticURL(entry.url) : undefined };
    this.entries.push(item);
    this.bytes += Buffer.byteLength(JSON.stringify(item));
    while (this.entries.length > 200 || this.bytes > 256 * 1024) {
      const shifted = this.entries.shift();
      if (!shifted) {
        this.bytes = 0;
        break;
      }
      this.bytes -= Buffer.byteLength(JSON.stringify(shifted));
    }
    for (const listener of this.listeners) listener(item);
  }
  read(after = 0, kind = "") { return { available: true, cursor: this.sequence, truncated: after < (this.entries[0]?.sequence ?? 1) - 1, entries: this.entries.filter(entry => entry.sequence > after && (!kind || entry.kind === kind)) }; }
}