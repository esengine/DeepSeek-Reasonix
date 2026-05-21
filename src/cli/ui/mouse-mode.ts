// Mouse-protocol selection for the TUI. Default: DECSET 1007 (alternate scroll
// mode). We DON'T need full mouse capture — the TUI never reacts to clicks or
// drags, only the wheel. `?1000h` was the old default and broke native text
// selection + right-click context menu for every user (#1337, #677, #1419) just
// to get the wheel working — net negative when the only thing reasonix uses
// the mouse for is scrolling chat history.
//
// `?1007h` (alternate scroll): the terminal translates wheel events into
// up/down (or PgUp/PgDn, depending on terminal) key sequences in the alt
// screen. Mouse buttons stay native — copy/paste/right-click all behave the
// way the terminal owner expects. Supported by Windows Terminal, iTerm2,
// GNOME Terminal, kitty, Alacritty, recent xterm. Terminals without it
// (macOS Terminal.app, very old emulators) fall back to the host terminal's
// own scrollback for history — still functional, just not in-app wheel scroll.
//
// Escape hatch: `REASONIX_MOUSE_MODE=sgr` restores the old `?1000h+?1006h`
// behavior for users on terminals where alternate scroll doesn't work and
// they prefer in-app wheel over native selection. `off` disables mouse
// reporting entirely (no in-app wheel, full native behavior).

type Mode = "alternate-scroll" | "sgr" | "off";

function readMode(): Mode {
  const raw = (process.env.REASONIX_MOUSE_MODE ?? "").toLowerCase();
  if (raw === "sgr") return "sgr";
  if (raw === "off") return "off";
  return "alternate-scroll";
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
