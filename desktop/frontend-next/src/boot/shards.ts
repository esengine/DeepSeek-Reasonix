const NS = "http://www.w3.org/2000/svg";
const COUNT = 7;

export interface Piece {
  el: SVGGElement;
  dx: number;
  dy: number;
  turn: number;
}

/** Cuts a copy of the glyph into wedges around an off-centre point, each one
 *  offset outward along its own bisector, so animating them home reads as the
 *  letter being struck together. Offsets are in the glyph's user units. */
export function shatter(svg: SVGSVGElement, glyph: SVGGraphicsElement) {
  const box = glyph.getBBox();
  const cx = box.x + box.width * (0.42 + Math.random() * 0.16);
  const cy = box.y + box.height * (0.4 + Math.random() * 0.2);
  const reach = Math.hypot(box.width, box.height) * 1.5;
  const span = (Math.PI * 2) / COUNT;
  const start = Math.random() * span;
  const cuts = Array.from({ length: COUNT }, (_, i) => start + (i + 0.25 + Math.random() * 0.5) * span);
  const defs = svg.querySelector("defs") ?? svg.insertBefore(document.createElementNS(NS, "defs"), svg.firstChild);
  const layer = document.createElementNS(NS, "g");
  layer.setAttribute("class", "bshards");
  const pieces: Piece[] = cuts.map((a0, i) => {
    const a1 = i === COUNT - 1 ? cuts[0] + Math.PI * 2 : cuts[i + 1];
    const am = (a0 + a1) / 2;
    const at = (a: number) => `${cx + Math.cos(a) * reach},${cy + Math.sin(a) * reach}`;
    const clip = document.createElementNS(NS, "clipPath");
    clip.id = `bshard${i}`;
    const poly = document.createElementNS(NS, "polygon");
    poly.setAttribute("points", `${cx},${cy} ${at(a0)} ${at(am)} ${at(a1)}`);
    clip.append(poly);
    defs.append(clip);
    const el = document.createElementNS(NS, "g");
    el.setAttribute("clip-path", `url(#bshard${i})`);
    const copy = glyph.cloneNode(true) as Element;
    copy.removeAttribute("class");
    copy.removeAttribute("style");
    el.append(copy);
    layer.append(el);
    const dist = box.width * (0.55 + Math.random() * 0.5);
    return { el, dx: Math.cos(am) * dist, dy: Math.sin(am) * dist, turn: (Math.random() - 0.5) * 24 };
  });
  glyph.after(layer);
  return { layer, pieces };
}
