export type Scalar = string | number | boolean;

export interface OpsEvent {
  id: number;
  ts: string;
  src: string;
  t: string;
  repo?: string;
  n?: number;
  title?: string;
  by?: string;
  url?: string;
  extra?: Record<string, Scalar>;
}

export type NewEvent = Omit<OpsEvent, "id" | "ts">;

export const MAX_FRAME_BYTES = 600;
const TITLE_MAX = 120;

// A title is the only free text an event carries; it is untrusted, so it is
// flattened to one line, stripped of control characters and bounded.
export function cleanTitle(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  let flat = "";
  for (const ch of value) {
    const code = ch.codePointAt(0) ?? 0;
    const control = code < 0x20 || (code >= 0x7f && code <= 0x9f) || code === 0x2028 || code === 0x2029;
    flat += control ? " " : ch;
  }
  flat = flat.replace(/\s+/g, " ").trim();
  if (!flat) return undefined;
  return flat.length > TITLE_MAX ? `${flat.slice(0, TITLE_MAX - 1)}…` : flat;
}

const encoder = new TextEncoder();

// Serialises one frame and shrinks the title until it fits; a frame never
// exceeds MAX_FRAME_BYTES, so a flood of long titles cannot grow the buffer.
export function frame(event: OpsEvent): string {
  let current = { ...event };
  let text = JSON.stringify(current);
  while (encoder.encode(text).length > MAX_FRAME_BYTES && current.title && current.title.length > 8) {
    current = { ...current, title: `${current.title.slice(0, Math.max(8, current.title.length - 20))}…` };
    text = JSON.stringify(current);
  }
  if (encoder.encode(text).length > MAX_FRAME_BYTES) {
    const { title: _title, extra: _extra, ...rest } = current;
    text = JSON.stringify(rest);
  }
  return text;
}
