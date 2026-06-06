import { forEachLine } from './_internal/iter-lines.js';
import { lineWidth } from './line-width-cache.js';

/** Visual width of the widest line in `text`. */
export function widestLine(text: string): number {
  let max = 0;
  forEachLine(text, (line) => {
    const w = lineWidth(line);
    if (w > max) max = w;
  });
  return max;
}
