// Mouse-protocol selection for the TUI. Reasonix only reacts to the wheel —
// clicks and drags are not handled — so the goal is "wheel works in-app,
// native click/drag/right-click stays untouched".
//
// `?1007h` (DECSET alternate scroll) does exactly that: the terminal
// translates wheel events into up/down (or PgUp/PgDn) key sequences in the
// alt screen, leaving mouse buttons native. Works on iTerm2, GNOME
// Terminal, kitty, Alacritty, recent xterm.
//
// Windows Terminal is the odd one out: it advertises `?1007h` but doesn't
// reliably translate wheel events to keys, so reasonix never sees the
// wheel. The old default `?1000h+?1006h` (full SGR mouse capture) DID work
// for the wheel on WT, at the cost of breaking native text selection /
// right-click — which is what got us into #1337 / #677 / #1419 in the
// first place. On Windows the proven wheel-working mode is still SGR, so
// platform-detect and pick the right one rather than ship a regression
// either way (#1456-followup: WT users lost wheel scroll after the
// alternate-scroll switch).
//
// Escape hatch: `REASONIX_MOUSE_MODE=sgr|alternate-scroll|off` overrides
// the platform default for users on terminals where the auto-pick is
// wrong.

type Mode = "alternate-scroll" | "sgr" | "off";

function platformDefault(): Mode {
  // Windows Terminal / conhost / ConEmu / Cmder all honor SGR mouse capture
  // but not alternate-scroll. The handful of Linux/macOS terminals that
  // don't honor `?1007h` (notably macOS Terminal.app) fall back to native
  // terminal scrollback, which is acceptable; broken in-app wheel for WT
  // users is not.
  return process.platform === "win32" ? "sgr" : "alternate-scroll";
}

function readMode(): Mode {
  const raw = (process.env.REASONIX_MOUSE_MODE ?? "").toLowerCase();
  if (raw === "sgr") return "sgr";
  if (raw === "alternate-scroll") return "alternate-scroll";
  if (raw === "off") return "off";
  return platformDefault();
}

const SEQUENCES: Record<Mode, { enable: string; disable: string }> = {
  "alternate-scroll": { enable: "\u001b[?1007h", disable: "\u001b[?1007l" },
  sgr: { enable: "\u001b[?1000h\u001b[?1006h", disable: "\u001b[?1006l\u001b[?1000l" },
  off: { enable: "", disable: "" },
};

let active = false;
let activeMode: Mode = "alternate-scroll";

export function enableMouseMode(): void {
  if (active) return;
  if (!process.stdout.isTTY) return;
  activeMode = readMode();
  const seq = SEQUENCES[activeMode].enable;
  if (seq) process.stdout.write(seq);
  active = true;
}

export function disableMouseMode(): void {
  if (!active) return;
  const seq = SEQUENCES[activeMode].disable;
  if (seq) process.stdout.write(seq);
  active = false;
}
