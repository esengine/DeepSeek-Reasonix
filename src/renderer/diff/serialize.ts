import type { Diff, Patch } from "./patch.js";

const ESC = "\x1b";
const CSI = `${ESC}[`;
const ST = `${ESC}\\`;

export function serializePatches(patches: Diff): string {
  let out = "";
  for (const patch of patches) {
    out += serializeOne(patch);
  }
  return out;
}

function serializeOne(patch: Patch): string {
  switch (patch.type) {
    case "stdout":
      return patch.content;
    case "cursorMove":
      return cursorMove(patch.dx, patch.dy);
    case "cursorTo":
      return `${CSI}${patch.col + 1}G`;
    case "cursorVisible":
      return patch.visible ? `${CSI}?25h` : `${CSI}?25l`;
    case "carriageReturn":
      return "\r";
    case "styleStr":
      return patch.str;
    case "hyperlink":
      return `${ESC}]8;;${patch.uri}${ST}`;
    case "clear":
      return clearLines(patch.count);
    case "clearTerminal":
      return `${CSI}2J${CSI}H`;
  }
}

function cursorMove(dx: number, dy: number): string {
  let out = "";
  if (dy > 0) out += `${CSI}${dy}B`;
  else if (dy < 0) out += `${CSI}${-dy}A`;
  if (dx > 0) out += `${CSI}${dx}C`;
  else if (dx < 0) out += `${CSI}${-dx}D`;
  return out;
}

function clearLines(count: number): string {
  if (count <= 0) return `\r${CSI}J`;
  return `\r${CSI}${count}A${CSI}J`;
}
