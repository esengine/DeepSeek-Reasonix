import { forEachLine } from './_internal/iter-lines.js';
import { lineWidth } from './line-width-cache.js';

type Measurement = {
  width: number;
  height: number;
};

/** Single-pass width/height measurement for a multi-line string. */
function measureText(text: string, maxWidth: number): Measurement {
  if (text.length === 0) {
    return { width: 0, height: 0 };
  }

  const noWrap = maxWidth <= 0 || !Number.isFinite(maxWidth);

  let width = 0;
  let height = 0;

  forEachLine(text, (line) => {
    const w = lineWidth(line);
    if (w > width) width = w;
    height += noWrap || w === 0 ? 1 : Math.ceil(w / maxWidth);
  });

  return { width, height };
}

export default measureText;
