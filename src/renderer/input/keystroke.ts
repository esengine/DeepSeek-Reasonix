export interface Keystroke {
  readonly input: string;
  readonly raw: string;
  readonly ctrl: boolean;
  readonly meta: boolean;
  readonly shift: boolean;
  readonly upArrow: boolean;
  readonly downArrow: boolean;
  readonly leftArrow: boolean;
  readonly rightArrow: boolean;
  readonly home: boolean;
  readonly end: boolean;
  readonly pageUp: boolean;
  readonly pageDown: boolean;
  readonly delete: boolean;
  readonly backspace: boolean;
  readonly return: boolean;
  readonly escape: boolean;
  readonly tab: boolean;
  readonly wheelUp: boolean;
  readonly wheelDown: boolean;
}

export function emptyKeystroke(): Keystroke {
  return {
    input: "",
    raw: "",
    ctrl: false,
    meta: false,
    shift: false,
    upArrow: false,
    downArrow: false,
    leftArrow: false,
    rightArrow: false,
    home: false,
    end: false,
    pageUp: false,
    pageDown: false,
    delete: false,
    backspace: false,
    return: false,
    escape: false,
    tab: false,
    wheelUp: false,
    wheelDown: false,
  };
}

const CSI_FINAL: Record<string, Partial<Keystroke>> = {
  A: { upArrow: true },
  B: { downArrow: true },
  C: { rightArrow: true },
  D: { leftArrow: true },
  H: { home: true },
  F: { end: true },
};

const CSI_TILDE: Record<string, Partial<Keystroke>> = {
  "1": { home: true },
  "3": { delete: true },
  "4": { end: true },
  "5": { pageUp: true },
  "6": { pageDown: true },
  "7": { home: true },
  "8": { end: true },
};

export function parseKeystrokes(chunk: string): Keystroke[] {
  const out: Keystroke[] = [];
  let i = 0;
  while (i < chunk.length) {
    const consumed = parseOne(chunk, i);
    if (consumed.length === 0) {
      i++;
      continue;
    }
    out.push(consumed.key);
    i += consumed.length;
  }
  return out;
}

interface Consumed {
  key: Keystroke;
  length: number;
}

function parseOne(s: string, start: number): Consumed {
  const ch = s[start]!;

  if (ch === "\x1b") {
    return parseEscape(s, start);
  }

  if (ch === "\r" || ch === "\n") {
    return { key: { ...emptyKeystroke(), raw: ch, return: true }, length: 1 };
  }
  if (ch === "\t") {
    return { key: { ...emptyKeystroke(), raw: ch, tab: true }, length: 1 };
  }
  if (ch === "\x7f" || ch === "\x08") {
    return { key: { ...emptyKeystroke(), raw: ch, backspace: true }, length: 1 };
  }

  const code = ch.charCodeAt(0);
  if (code >= 1 && code <= 26 && ch !== "\t" && ch !== "\r" && ch !== "\n") {
    const letter = String.fromCharCode(code + 96);
    return {
      key: { ...emptyKeystroke(), raw: ch, ctrl: true, input: letter },
      length: 1,
    };
  }

  return { key: { ...emptyKeystroke(), raw: ch, input: ch }, length: 1 };
}

function parseEscape(s: string, start: number): Consumed {
  if (start + 1 >= s.length) {
    return { key: { ...emptyKeystroke(), raw: "\x1b", escape: true }, length: 1 };
  }

  const next = s[start + 1]!;

  if (next === "[" || next === "O") {
    return parseCsi(s, start);
  }

  if (isPrintable(next)) {
    return {
      key: { ...emptyKeystroke(), raw: `\x1b${next}`, meta: true, input: next },
      length: 2,
    };
  }

  return { key: { ...emptyKeystroke(), raw: "\x1b", escape: true }, length: 1 };
}

function parseCsi(s: string, start: number): Consumed {
  let i = start + 2;
  let params = "";
  while (i < s.length) {
    const c = s[i]!;
    if (isCsiFinal(c)) {
      const raw = s.slice(start, i + 1);
      const key = decodeCsi(params, c, raw);
      return { key, length: i - start + 1 };
    }
    params += c;
    i++;
  }
  return {
    key: { ...emptyKeystroke(), raw: s.slice(start), escape: true },
    length: s.length - start,
  };
}

function decodeCsi(params: string, final: string, raw: string): Keystroke {
  const base = { ...emptyKeystroke(), raw };

  if (params.startsWith("<") && (final === "M" || final === "m")) {
    const mouse = params.slice(1).split(";");
    const button = Number.parseInt(mouse[0] ?? "", 10);
    if (button === 64) return { ...base, wheelUp: true };
    if (button === 65) return { ...base, wheelDown: true };
    return base;
  }

  const segments = params.split(";");
  const modCode = segments[1] ? Number.parseInt(segments[1], 10) : 1;
  const mods = decodeModifiers(modCode);

  if (final === "~" && segments[0]) {
    const part = CSI_TILDE[segments[0]];
    if (part) return { ...base, ...part, ...mods };
  }

  const part = CSI_FINAL[final];
  if (part) return { ...base, ...part, ...mods };

  return { ...base, escape: true };
}

function decodeModifiers(code: number): Pick<Keystroke, "shift" | "meta" | "ctrl"> {
  const m = code - 1;
  return {
    shift: (m & 1) !== 0,
    meta: (m & 2) !== 0,
    ctrl: (m & 4) !== 0,
  };
}

function isCsiFinal(ch: string): boolean {
  const code = ch.charCodeAt(0);
  return code >= 0x40 && code <= 0x7e && ch !== "[";
}

function isPrintable(ch: string): boolean {
  const code = ch.charCodeAt(0);
  return code >= 0x20 && code <= 0x7e;
}
