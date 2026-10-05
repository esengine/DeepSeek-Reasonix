// 最小 VLQ 解码：把打包产物的 (行,列) 还原成源码位置。
import { readFileSync } from "node:fs";
const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
function decode(seg) {
  const out = [];
  let shift = 0, value = 0;
  for (const ch of seg) {
    const d = B64.indexOf(ch);
    if (d < 0) throw new Error("bad vlq " + ch);
    value += (d & 31) << shift;
    if (d & 32) { shift += 5; continue; }
    const neg = value & 1;
    value >>= 1;
    out.push(neg ? -value : value);
    shift = 0; value = 0;
  }
  return out;
}
export function loadMap(path) {
  const m = JSON.parse(readFileSync(path, "utf8"));
  const lines = [];
  let src = 0, sl = 0, sc = 0, nm = 0;
  m.mappings.split(";").forEach((line, li) => {
    let col = 0;
    const segs = [];
    for (const seg of line.split(",")) {
      if (!seg) continue;
      const f = decode(seg);
      col += f[0];
      if (f.length >= 4) { src += f[1]; sl += f[2]; sc += f[3]; }
      if (f.length >= 5) nm += f[4];
      segs.push({ col, src: m.sources[src], line: sl + 1, name: f.length >= 5 ? m.names[nm] : null });
    }
    segs.sort((a, b) => a.col - b.col);
    lines[li] = segs;
  });
  return {
    at(line, col) {
      const segs = lines[line] ?? [];
      let best = null;
      for (const s of segs) { if (s.col <= col) best = s; else break; }
      return best;
    },
  };
}
