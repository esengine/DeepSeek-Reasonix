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

const bytesOf = (text: string) => encoder.encode(text).length;

// Serialises one frame within MAX_FRAME_BYTES in a bounded number of steps:
// drop extra, then cut the title by bytes, then drop the title and the optional
// routing fields. A frame never exceeds the limit, so a flood of long events
// cannot grow the buffer.
export function frame(event: OpsEvent): string {
  const whole = JSON.stringify(event);
  if (bytesOf(whole) <= MAX_FRAME_BYTES) return whole;
  const { extra: _extra, title, ...noExtra } = event;
  if (title) {
    const withTitle = (t: string) => JSON.stringify({ ...noExtra, title: t });
    const chars = Array.from(title);
    for (let keep = chars.length; keep >= 1; keep--) {
      const cut = keep === chars.length ? title : `${chars.slice(0, keep - 1).join("")}…`;
      const text = withTitle(cut);
      if (bytesOf(text) <= MAX_FRAME_BYTES) return text;
    }
  }
  const bare = JSON.stringify(noExtra);
  if (bytesOf(bare) <= MAX_FRAME_BYTES) return bare;
  const { id, ts, src, t } = event;
  return JSON.stringify({ id, ts, src, t });
}
