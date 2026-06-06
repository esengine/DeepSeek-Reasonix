import sliceAnsi from './_internal/sliceAnsi.js';
import { stringWidth } from './stringWidth.js';
import type { Styles } from './styles.js';
import { wrapAnsi } from './wrapAnsi.js';

const ELLIPSIS = '…';

function sliceFit(text: string, start: number, end: number): string {
  const slice = sliceAnsi(text, start, end);
  return stringWidth(slice) > end - start ? sliceAnsi(text, start, end - 1) : slice;
}

function truncate(text: string, columns: number, position: 'start' | 'middle' | 'end'): string {
  if (columns < 1) return '';
  if (columns === 1) return ELLIPSIS;

  const length = stringWidth(text);
  if (length <= columns) return text;

  if (position === 'start') {
    return ELLIPSIS + sliceFit(text, length - columns + 1, length);
  }

  if (position === 'middle') {
    const leadingHalf = Math.floor(columns / 2);
    const trailingHalf = columns - leadingHalf;
    return (
      sliceFit(text, 0, leadingHalf) +
      ELLIPSIS +
      sliceFit(text, length - trailingHalf + 1, length)
    );
  }

  return sliceFit(text, 0, columns - 1) + ELLIPSIS;
}

/** Apply the layout's `textWrap` policy to a single text node's content. */
export default function wrapText(text: string, maxWidth: number, wrapType: Styles['textWrap']): string {
  if (wrapType === 'wrap') {
    return wrapAnsi(text, maxWidth, { trim: false, hard: true });
  }

  if (wrapType === 'wrap-trim') {
    return wrapAnsi(text, maxWidth, { trim: true, hard: true });
  }

  if (wrapType!.startsWith('truncate')) {
    let position: 'end' | 'middle' | 'start' = 'end';
    if (wrapType === 'truncate-middle') position = 'middle';
    if (wrapType === 'truncate-start') position = 'start';
    return truncate(text, maxWidth, position);
  }

  return text;
}
