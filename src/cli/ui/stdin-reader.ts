/** Sole stdin owner; 250 ms ESC-ambiguity timer (ConPTY splits sequences past parse-keypress's 100 ms). */

import { stdin } from "node:process";

export interface KeyEvent {
  /** Empty for control keys (arrows / Enter / Esc); holds the letter for Ctrl+/Alt+. */
  input: string;
  upArrow?: boolean;
  downArrow?: boolean;
  leftArrow?: boolean;
  rightArrow?: boolean;
  pageUp?: boolean;
  pageDown?: boolean;
  home?: boolean;
  end?: boolean;
  delete?: boolean;
  backspace?: boolean;
  tab?: boolean;
  return?: boolean;
  escape?: boolean;
  shift?: boolean;
  ctrl?: boolean;
  meta?: boolean;
  /** Bracketed-paste content; consumers MUST NOT re-interpret as keystrokes (e.g. `\n` ≠ submit). */
  paste?: boolean;
  /** xterm SGR mode 1006 wheel-up. */
  mouseScrollUp?: boolean;
  /** Mouse wheel down — symmetric to `mouseScrollUp`. */
  mouseScrollDown?: boolean;
  /** Left-button press; row/col are 1-based. */
  mouseClick?: boolean;
  /** Left-button motion (button held during drag). Mode 1002 only. */
  mouseDrag?: boolean;
  /** Any-button release. Mode 1002 only. */
  mouseRelease?: boolean;
  mouseRow?: number;
  mouseCol?: number;
}

type Subscriber = (ev: KeyEvent) => void;

/** ESC ambiguity timeout. Long enough for ConPTY-split sequences. */
const ESC_TIMEOUT_MS = 250;

/** Bracketed-paste markers (DECSET 2004). */
const PASTE_START = "\x1b[200~";
const PASTE_END = "\x1b[201~";
/** ESC-stripped variants — ConPTY occasionally eats the leading ESC. */
const PASTE_START_BARE = "[200~";
const PASTE_END_BARE = "[201~";

const CSI_TAIL_MAP: ReadonlyArray<{ tail: string; ev: KeyEvent }> = [
  { tail: "A", ev: { input: "", upArrow: true } },
  { tail: "B", ev: { input: "", downArrow: true } },
  { tail: "C", ev: { input: "", rightArrow: true } },
  { tail: "D", ev: { input: "", leftArrow: true } },
  { tail: "H", ev: { input: "", home: true } },
  { tail: "F", ev: { input: "", end: true } },
  { tail: "1~", ev: { input: "", home: true } },
  { tail: "4~", ev: { input: "", end: true } },
  { tail: "5~", ev: { input: "", pageUp: true } },
  { tail: "6~", ev: { input: "", pageDown: true } },
  { tail: "3~", ev: { input: "", delete: true } },
  { tail: "Z", ev: { input: "", shift: true, tab: true } },
  // Some Windows hosts (PowerShell 7.x conhost path) emit the
  // modifier-encoded back-tab `\x1b[1;2Z` instead of bare `\x1b[Z`.
  // Issue #373 — without this entry Shift+Tab is silently dropped.
  { tail: "1;2Z", ev: { input: "", shift: true, tab: true } },
  // modifyOtherKeys (xterm CSI > 4 ; 2 m) sequences for Enter / Tab
  // with modifiers. Only fired when App.tsx has enabled the mode at
  // startup; otherwise Shift+Enter stays indistinguishable from Enter.
  // Modifier encoding: 2=shift, 3=alt, 4=alt+shift, 5=ctrl,
  // 6=ctrl+shift, 7=ctrl+alt, 8=ctrl+alt+shift. Keycodes: 9=Tab, 13=Enter.
  { tail: "27;2;9~", ev: { input: "", tab: true, shift: true } },
  { tail: "27;2;13~", ev: { input: "", return: true, shift: true } },
  { tail: "27;5;13~", ev: { input: "", return: true, ctrl: true } },
  { tail: "27;6;13~", ev: { input: "", return: true, ctrl: true, shift: true } },
  // Kitty keyboard protocol — same idea, different envelope:
  // `\x1b[<keycode>;<mod>u`. Some terminals (kitty, recent Windows
  // Terminal previews) prefer this shape. Harmless to map here too.
  { tail: "9;2u", ev: { input: "", tab: true, shift: true } },
  { tail: "13;2u", ev: { input: "", return: true, shift: true } },
  { tail: "13;5u", ev: { input: "", return: true, ctrl: true } },
  { tail: "13;6u", ev: { input: "", return: true, ctrl: true, shift: true } },
];

/** SS3 sequences (`\x1bO<letter>`) — some terminals send these for arrows. */
const SS3_MAP: Record<string, KeyEvent> = {
  A: { input: "", upArrow: true },
  B: { input: "", downArrow: true },
  C: { input: "", rightArrow: true },
  D: { input: "", leftArrow: true },
  H: { input: "", home: true },
  F: { input: "", end: true },
};

/** ESC-stripped CSI lookahead — ConPTY occasionally drops the leading ESC. */
function tryEscapelessCsi(chunk: string, i: number): { advance: number; ev: KeyEvent } | null {
  if (chunk[i] !== "[") return null;
  // Paste start as a special case (handled by caller).
  // Try each known tail.
  for (const entry of CSI_TAIL_MAP) {
    const candidate = `[${entry.tail}`;
    if (chunk.slice(i, i + candidate.length) === candidate) {
      return { advance: candidate.length, ev: entry.ev };
    }
  }
  return null;
}

/** `[<btn;col;row[Mm]` — SGR mouse report body (without leading ESC). */
const SGR_MOUSE_ESCAPELESS_RE = /^\[<\d+;\d+;\d+[Mm]/;

function decodeSgrMouseBody(body: string): KeyEvent | null {
  const m = /^<(\d+);(\d+);(\d+)([Mm])$/.exec(body);
  if (!m) return null;
  const btn = Number.parseInt(m[1]!, 10);
  const col = Number.parseInt(m[2]!, 10);
  const row = Number.parseInt(m[3]!, 10);
  if (!Number.isFinite(btn) || !Number.isFinite(col) || !Number.isFinite(row)) return null;
  const tail = m[4]!;
  if (tail === "m") return { input: "", mouseRelease: true, mouseRow: row, mouseCol: col };
  if (btn === 64) return { input: "", mouseScrollUp: true, mouseRow: row, mouseCol: col };
  if (btn === 65) return { input: "", mouseScrollDown: true, mouseRow: row, mouseCol: col };
  if (btn === 0) return { input: "", mouseClick: true, mouseRow: row, mouseCol: col };
  if (btn === 32) return { input: "", mouseDrag: true, mouseRow: row, mouseCol: col };
  return null;
}

/** ConPTY can strip the ESC off SGR mouse reports — match the bare shape and drop, issue #867. */
function tryEscapelessSgrMouse(
  chunk: string,
  i: number,
): { advance: number; ev: KeyEvent | null } | null {
  if (chunk[i] !== "[") return null;
  const m = SGR_MOUSE_ESCAPELESS_RE.exec(chunk.slice(i));
  if (!m) return null;
  const body = m[0].slice(1);
  return { advance: m[0].length, ev: decodeSgrMouseBody(body) };
}

function isCsiFinal(ch: string): boolean {
  const code = ch.charCodeAt(0);
  return code >= 0x40 && code <= 0x7e;
}

/** Unknown sequence → null → caller drops bytes silently (don't insert as text). */
function lookupCsi(tail: string): KeyEvent | null {
  for (const entry of CSI_TAIL_MAP) {
    if (entry.tail === tail) return entry.ev;
  }
  return null;
}

/** modifyOtherKeys / Kitty: reconstruct the keystroke from `<codepoint>` + `<mod>`. */
function decodeModifiedKey(cp: number, mod: number): KeyEvent | null {
  if (mod < 1 || mod > 8) return null;
  const bits = mod - 1;
  const shift = (bits & 1) !== 0;
  const alt = (bits & 2) !== 0;
  const ctrl = (bits & 4) !== 0;
  if (cp >= 0x20 && cp <= 0x7e && !ctrl && !alt) {
    const ev: KeyEvent = { input: String.fromCharCode(cp) };
    if (shift) ev.shift = true;
    return ev;
  }
  if (cp >= 0x20 && cp <= 0x7e && alt && !ctrl) {
    const ev: KeyEvent = { input: String.fromCharCode(cp), meta: true };
    if (shift) ev.shift = true;
    return ev;
  }
  if (cp >= 0x41 && cp <= 0x7a && ctrl && !alt) {
    const ev: KeyEvent = { input: String.fromCharCode(cp).toLowerCase(), ctrl: true };
    if (shift) ev.shift = true;
    return ev;
  }
  return null;
}

/** Generic modifyOtherKeys / Kitty envelope — picks up the keys lookupCsi misses (`@`, `_`, `[`, `\`, `]`, `^` under `>4;2m`). */
function tryDecodeGenericCsi(seq: string): KeyEvent | null {
  let m = /^27;(\d+);(\d+)~$/.exec(seq);
  if (m) return decodeModifiedKey(Number.parseInt(m[2]!, 10), Number.parseInt(m[1]!, 10));
  m = /^(\d+);(\d+)u$/.exec(seq);
  if (m) return decodeModifiedKey(Number.parseInt(m[1]!, 10), Number.parseInt(m[2]!, 10));
  m = /^(\d+)u$/.exec(seq);
  if (m) return decodeModifiedKey(Number.parseInt(m[1]!, 10), 1);
  return null;
}

// Bidi controls + zero-width invisibles that browsers smuggle into the clipboard (e.g. a B-site tab title with RLE/PDF wrappers). They render as 0 cells but still occupy buffer offsets, so cursor + line-split math drifts. ZWJ / ZWNJ / variation selectors / combining marks are NOT in the class — emoji sequences and accented letters keep their semantics. Issue #849.
const PASTE_INVISIBLE_RE = /[\u200B\u200E\u200F\u202A-\u202E\u2060\u2066-\u2069\u00AD\uFEFF]/g;

export function sanitizePasteText(s: string): string {
  // `ev.paste` bypasses the multiline reducer, so normalize Windows
  // clipboard line endings here before raw CR can reach Ink's <Text>.
  return s.replace(PASTE_INVISIBLE_RE, "").replace(/\r\n?/g, "\n");
}

/** Heuristic paste-burst detector — wraps raw multi-line chunks when the terminal didn't (#522). */
export function looksLikeUnbracketedPaste(chunk: string): boolean {
  if (chunk.length < 2) return false;
  if (chunk.includes(PASTE_START) || chunk.includes(PASTE_START_BARE)) return false;
  if (chunk.includes(PASTE_END) || chunk.includes(PASTE_END_BARE)) return false;
  // ESC anywhere = real keypress / control sequence, not a paste burst.
  if (chunk.includes("\x1b")) return false;
  // \r\n is one terminal-converted Enter, not two breaks — fold first.
  const norm = chunk.replace(/\r\n/g, "\n");
  if (norm === "\r" || norm === "\n") return false;
  let breaks = 0;
  let firstBreakIdx = -1;
  for (let i = 0; i < norm.length; i++) {
    const c = norm[i];
    if (c === "\r" || c === "\n") {
      if (firstBreakIdx < 0) firstBreakIdx = i;
      breaks++;
    }
  }
  if (breaks >= 2) return true;
  // 1 break with non-empty text on BOTH sides — paste burst. ("abc\r"
  // alone stays as type-then-Enter so a fast typist still submits.)
  if (breaks === 1) return firstBreakIdx > 0 && firstBreakIdx < norm.length - 1;
  return false;
}

export class StdinReader {
  private subscribers = new Set<Subscriber>();
  private state: "idle" | "esc" | "csi" | "ss3" | "paste" = "idle";
  /** Buffer for partial sequences across chunks. */
  private csiBuf = "";
  /** Buffer for paste content. */
  private pasteBuf = "";
  private escTimer: NodeJS.Timeout | null = null;
  // Deferred-dispatch handle paired with `escTimer`. The timer
  // queues an Immediate that runs in the event loop's CHECK phase —
  // i.e. AFTER the POLL phase where stdin 'data' events fire — so
  // a multi-byte sequence whose chunks queued up while the loop was
  // blocked (heavy render, etc.) gets a chance to be processed
  // BEFORE we emit a bogus standalone-Esc. Fixes the "I didn't press
  // Esc but it aborted the turn" class of bug: previously the timer's
  // setTimeout callback ran in the timers phase ahead of poll, so a
  // split sequence like `\x1b` + `[A` would dispatch escape+upArrow
  // even though the user only pressed Up.
  private escImmediate: NodeJS.Immediate | null = null;
  private started = false;
  /** The actual `data` listener — kept as a field so `stop()` can detach it. */
  private listener: ((chunk: Buffer | string) => void) | null = null;

  start(): void {
    if (this.started) return;
    // bun leaves `isTTY` undefined in a real terminal, so probe setRawMode directly.
    try {
      stdin.setRawMode(true);
    } catch {
      return;
    }
    stdin.setEncoding("utf8");
    stdin.resume();
    this.listener = (chunk) =>
      this.handleChunk(typeof chunk === "string" ? chunk : chunk.toString("utf8"));
    stdin.on("data", this.listener);
    this.started = true;
  }

  stop(): void {
    if (!this.started) return;
    if (this.listener) {
      stdin.off("data", this.listener);
      this.listener = null;
    }
    try {
      stdin.setRawMode(false);
    } catch {
      // setRawMode may throw if stdin is already closed; ignore.
    }
    stdin.pause();
    this.cancelEscTimer();
    this.state = "idle";
    this.csiBuf = "";
    this.pasteBuf = "";
    this.started = false;
  }

  subscribe(fn: Subscriber): () => void {
    this.subscribers.add(fn);
    return () => {
      this.subscribers.delete(fn);
    };
  }

  /** Test seam — drives the parser without a real TTY. */
  feed(chunk: string): void {
    this.handleChunk(chunk);
  }

  private dispatch(ev: KeyEvent): void {
    for (const sub of this.subscribers) sub(ev);
  }

  private cancelEscTimer(): void {
    if (this.escTimer) {
      clearTimeout(this.escTimer);
      this.escTimer = null;
    }
    if (this.escImmediate) {
      clearImmediate(this.escImmediate);
      this.escImmediate = null;
    }
  }

  private scheduleEscTimer(): void {
    this.cancelEscTimer();
    this.escTimer = setTimeout(() => {
      this.escTimer = null;
      // Defer the actual dispatch to the CHECK phase so any pending
      // stdin 'data' events that queued up during a long render still
      // get a chance to consume the rest of a split sequence. The
      // chunk handler cancels this Immediate at its start, so a
      // sequence completing first wins; only a truly-orphaned `\x1b`
      // reaches the dispatch below.
      this.escImmediate = setImmediate(() => {
        this.escImmediate = null;
        if (this.state === "esc") {
          this.state = "idle";
          this.dispatch({ input: "", escape: true });
        }
      });
    }, ESC_TIMEOUT_MS);
  }

  private handleChunk(rawChunk: string): void {
    this.cancelEscTimer();
    // Paste rescue when DECSET 2004 markers don't arrive (multiplexers
    // strip them, some Windows pipes too) — otherwise each \r in a
    // multi-line paste fires Enter and the loop submits N prompts (#522).
    const chunk =
      this.state === "idle" && looksLikeUnbracketedPaste(rawChunk)
        ? PASTE_START + rawChunk + PASTE_END
        : rawChunk;
    let i = 0;
    while (i < chunk.length) {
      // ── paste accumulator ──
      if (this.state === "paste") {
        // Look for end marker (with or without ESC).
        const endA = chunk.indexOf(PASTE_END, i);
        const endB = chunk.indexOf(PASTE_END_BARE, i);
        let endIdx = -1;
        let endLen = 0;
        if (endA !== -1 && (endB === -1 || endA <= endB)) {
          endIdx = endA;
          endLen = PASTE_END.length;
        } else if (endB !== -1) {
          endIdx = endB;
          endLen = PASTE_END_BARE.length;
        }
        if (endIdx === -1) {
          this.pasteBuf += chunk.slice(i);
          i = chunk.length;
          break;
        }
        this.pasteBuf += chunk.slice(i, endIdx);
        this.dispatch({ input: sanitizePasteText(this.pasteBuf), paste: true });
        this.pasteBuf = "";
        this.state = "idle";
        i = endIdx + endLen;
        continue;
      }

      // ── CSI accumulator ──
      if (this.state === "csi") {
        const ch = chunk[i]!;
        this.csiBuf += ch;
        if (isCsiFinal(ch)) {
          this.dispatchCsi(this.csiBuf);
          this.csiBuf = "";
          // Only reset state if `dispatchCsi` didn't already mutate it
          // (it transitions to `paste` for the `200~` start marker —
          // resetting here would clobber that and the paste content
          // would be parsed as keystrokes).
          if (this.state === "csi") this.state = "idle";
        }
        i++;
        continue;
      }

      // ── SS3 single-byte tail ──
      if (this.state === "ss3") {
        const ev = SS3_MAP[chunk[i]!];
        if (ev) this.dispatch(ev);
        this.state = "idle";
        i++;
        continue;
      }

      // ── ESC pending ──
      if (this.state === "esc") {
        const ch = chunk[i]!;
        if (ch === "[") {
          this.state = "csi";
          this.csiBuf = "";
          i++;
          continue;
        }
        if (ch === "O") {
          this.state = "ss3";
          i++;
          continue;
        }
        // Alt+Enter: ESC + CR (or ESC + LF). Universal newline shortcut on terminals
        // that don't support modifyOtherKeys (Shift+Enter falls through to plain Enter there).
        if (ch === "\r" || ch === "\n") {
          this.dispatch({ input: "", return: true, meta: true });
          this.state = "idle";
          i++;
          continue;
        }
        // ESC + any other char = Alt+key (rare; we still dispatch).
        this.dispatch({ input: ch, meta: true });
        this.state = "idle";
        i++;
        continue;
      }

      // ── idle ──
      const ch = chunk[i]!;

      if (ch === "\x1b") {
        this.state = "esc";
        i++;
        continue;
      }

      // ESC-stripped paste-start (ConPTY): bare `[200~` at idle.
      if (chunk.slice(i, i + PASTE_START_BARE.length) === PASTE_START_BARE) {
        this.state = "paste";
        this.pasteBuf = "";
        i += PASTE_START_BARE.length;
        continue;
      }
      // ESC-stripped CSI tails — recover before treating `[` as text.
      const escapeless = tryEscapelessCsi(chunk, i);
      if (escapeless) {
        this.dispatch(escapeless.ev);
        i += escapeless.advance;
        continue;
      }
      const mouseEscapeless = tryEscapelessSgrMouse(chunk, i);
      if (mouseEscapeless) {
        if (mouseEscapeless.ev) this.dispatch(mouseEscapeless.ev);
        i += mouseEscapeless.advance;
        continue;
      }

      // Single-byte control keys.
      // \r (CR, 0x0D) is Enter on every terminal in raw mode.
      // \n (LF, 0x0A) is what Ctrl+J emits — keep it distinct so the
      // multiline reducer can map it to "insert newline" instead of
      // "submit". Pastes containing \n still arrive via either the
      // bracketed-paste accumulator or a multi-byte printable chunk
      // that includes the newline; neither hits this single-byte
      // branch, so this split is safe.
      if (ch === "\r") {
        this.dispatch({ input: "", return: true });
        i++;
        continue;
      }
      if (ch === "\n") {
        this.dispatch({ input: "j", ctrl: true });
        i++;
        continue;
      }
      if (ch === "\t") {
        this.dispatch({ input: "", tab: true });
        i++;
        continue;
      }
      if (ch === "\x7f" || ch === "\b") {
        this.dispatch({ input: "", backspace: true });
        i++;
        continue;
      }
      if (ch === "\x03") {
        // Ctrl+C — terminate the process. Raw mode disables the
        // default SIGINT, so we have to handle it ourselves.
        this.dispatch({ input: "c", ctrl: true });
        i++;
        continue;
      }

      const code = ch.charCodeAt(0);
      // Other Ctrl+letter (0x01-0x1A → A-Z, except already-handled).
      if (code >= 1 && code <= 26) {
        const letter = String.fromCharCode(0x60 + code); // a..z
        this.dispatch({ input: letter, ctrl: true });
        i++;
        continue;
      }

      // Regular printable input. Coalesce a run of printable chars
      // into one event so a multi-byte UTF-8 paste-burst arrives as
      // one `input` rather than N adjacent events.
      let end = i + 1;
      while (end < chunk.length) {
        const c = chunk[end]!;
        if (c === "\x1b" || c === "\r" || c === "\n" || c === "\t") break;
        if (c === "\x7f" || c === "\b" || c === "\x03") break;
        const cc = c.charCodeAt(0);
        if (cc >= 1 && cc <= 26) break;
        // Don't swallow into a printable run if a CSI / paste prefix
        // starts at this position.
        if (c === "[" && (tryEscapelessCsi(chunk, end) || tryEscapelessSgrMouse(chunk, end))) break;
        if (chunk.slice(end, end + PASTE_START_BARE.length) === PASTE_START_BARE) break;
        end++;
      }
      this.dispatch({ input: chunk.slice(i, end) });
      i = end;
    }

    // After processing, if we're still in `esc` state, schedule the
    // ambiguity timer. The next chunk may carry the rest of the CSI;
    // if not, the timer fires and dispatches a standalone Esc.
    if (this.state === "esc") {
      this.scheduleEscTimer();
    }
  }

  private dispatchCsi(seq: string): void {
    // seq is the bytes after `\x1b[`, e.g. "A", "5~", "200~", "Z".
    if (seq === "200~") {
      this.state = "paste";
      this.pasteBuf = "";
      return;
    }
    if (seq === "201~") {
      // Stray paste-end — we shouldn't reach here outside paste mode,
      // but if we do, drop it silently.
      return;
    }
    // SGR mouse report — surface wheel/click/drag/release, drop the rest. Always consumes the bytes even when the button isn't one we map (issue #867).
    if (seq.length > 1 && seq.charCodeAt(0) === 60 /* '<' */) {
      const ev = decodeSgrMouseBody(seq);
      if (ev) this.dispatch(ev);
      return;
    }
    const ev = lookupCsi(seq);
    if (ev) {
      this.dispatch(ev);
      return;
    }
    const generic = tryDecodeGenericCsi(seq);
    if (generic) {
      this.dispatch(generic);
      return;
    }
    // Unknown CSI → drop. Do NOT insert raw bytes as text.
  }
}

/** Singleton — one reader per process. */
let singleton: StdinReader | null = null;

export function getStdinReader(): StdinReader {
  if (!singleton) singleton = new StdinReader();
  return singleton;
}
