// What this machine remembers about the terminal: it presents and starts shells
// here, so none of it is a fact about the session.

const NOTE = "rx-terminal-note";
const SHELL = "rx-terminal-shell";
const SIZE = "rx-terminal-size";

export const TERMINAL_SIZES = { min: 9, standard: 12, max: 22 } as const;

function read(key: string): string {
  try {
    return localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // A private window keeps the defaults, which is the answer it gives anyway.
  }
}

export const noteSeen = () => read(NOTE) === "seen";
export const seeNote = () => write(NOTE, "seen");

export const lastShell = () => read(SHELL);
export const rememberShell = (kind: string) => write(SHELL, kind);

export function terminalSize(): number {
  const n = Number(read(SIZE));
  return Number.isFinite(n) && n >= TERMINAL_SIZES.min && n <= TERMINAL_SIZES.max ? n : TERMINAL_SIZES.standard;
}

export const setTerminalSize = (n: number) => write(SIZE, String(Math.min(TERMINAL_SIZES.max, Math.max(TERMINAL_SIZES.min, Math.round(n)))));
