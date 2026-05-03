import type { ReactNode } from "react";
import type { DiffPools } from "../diff/diff-frames.js";
import { render as renderReactTree } from "../react/render.js";
import { CellWidth } from "../screen/cell.js";

const ESC = "\x1b";
const ST = `${ESC}\\`;

export function renderToBytes(element: ReactNode, width: number, pools: DiffPools): string {
  const screen = renderReactTree(element, { width, pools });
  if (screen.height === 0) return "";

  let out = "";
  let curStyle = pools.style.none;
  let curLink = 0;

  for (let y = 0; y < screen.height; y++) {
    if (y > 0) {
      out += pools.style.transition(curStyle, pools.style.none);
      curStyle = pools.style.none;
      if (curLink !== 0) {
        out += `${ESC}]8;;${ST}`;
        curLink = 0;
      }
      out += "\r\n";
    }
    for (let x = 0; x < screen.width; x++) {
      const cell = screen.cellAt(x, y);
      if (!cell) continue;
      if (cell.width === CellWidth.SpacerTail) continue;
      if (cell.styleId !== curStyle) {
        out += pools.style.transition(curStyle, cell.styleId);
        curStyle = cell.styleId;
      }
      if (cell.hyperlinkId !== curLink) {
        const uri = pools.hyperlink.get(cell.hyperlinkId) ?? "";
        out += `${ESC}]8;;${uri}${ST}`;
        curLink = cell.hyperlinkId;
      }
      out += pools.char.get(cell.charId);
    }
  }

  out += pools.style.transition(curStyle, pools.style.none);
  if (curLink !== 0) out += `${ESC}]8;;${ST}`;
  out += "\r\n";
  return out;
}
