import type { Diff } from './frame.js';

/** Compact a frame diff in a single forward pass. */
export function optimize(diff: Diff): Diff {
  if (diff.length <= 1) return diff;

  const result: Diff = [];
  let len = 0;

  for (const patch of diff) {
    const type = patch.type;

    // Drop no-op patches before considering any merge.
    if (type === 'stdout') {
      if (patch.content === '') continue;
    } else if (type === 'cursorMove') {
      if (patch.x === 0 && patch.y === 0) continue;
    } else if (type === 'clear') {
      if (patch.count === 0) continue;
    }

    if (len > 0) {
      const lastIdx = len - 1;
      const last = result[lastIdx]!;
      const lastType = last.type;

      if (type === 'cursorMove' && lastType === 'cursorMove') {
        result[lastIdx] = {
          type: 'cursorMove',
          x: last.x + patch.x,
          y: last.y + patch.y,
        };
        continue;
      }

      if (type === 'cursorTo' && lastType === 'cursorTo') {
        result[lastIdx] = patch;
        continue;
      }

      if (type === 'styleStr' && lastType === 'styleStr') {
        result[lastIdx] = { type: 'styleStr', str: last.str + patch.str };
        continue;
      }

      if (type === 'hyperlink' && lastType === 'hyperlink' && patch.uri === last.uri) {
        continue;
      }

      if (
        (type === 'cursorShow' && lastType === 'cursorHide') ||
        (type === 'cursorHide' && lastType === 'cursorShow')
      ) {
        result.pop();
        len--;
        continue;
      }
    }

    result.push(patch);
    len++;
  }

  return result;
}
