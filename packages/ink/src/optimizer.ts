import type { Diff } from './frame.js';
import { cursorMove } from './termio/csi.js';

/** Compact a frame diff in a single forward pass. */
export function optimize(diff: Diff): Diff {
  if (diff.length <= 1) return diff;

  const result: Diff = [];
  let len = 0;

  // Merge consecutive cursorMove patches first (original pass).
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

  // Second pass: fold (cursorMove|carriageReturn) → (styleStr)* → stdout
  // into a single stdout patch.  This collapses the common per-cell diff
  //   [CR, stdout("X"), stdout("Y"), CR, stdout("Z"), …]
  //   [cursorMove, styleStr?, stdout("X"), cursorMove, …]
  // into fewer, larger stdout writes.  Fewer patches means fewer escape
  // sequences for the terminal to parse, which reduces visible per-cell
  // painting on terminals that lack DEC 2026 (synchronized output).
  if (result.length <= 1) return result;

  const merged: Diff = [];
  let i = 0;
  while (i < result.length) {
    const patch = result[i]!;
    const isCursorOp = patch.type === 'cursorMove' || patch.type === 'carriageReturn';

    if (!isCursorOp) {
      // Non-cursor patch — check if it's a stdout that can merge with
      // preceding stdout (no cursor-op between them).
      if (patch.type === 'stdout' && merged.length > 0) {
        const prev = merged[merged.length - 1]!;
        if (prev.type === 'stdout') {
          (prev as { content: string }).content += patch.content;
          i++;
          continue;
        }
      }
      merged.push(patch);
      i++;
      continue;
    }

    // Accumulate cursor ops and styleStr patches until we hit a stdout.
    let accContent = '';
    let styleBuf = '';
    let j = i + 1;
    let foundStdout = false;

    if (patch.type === 'cursorMove') {
      accContent = cursorMove(patch.x, patch.y);
    } else {
      // carriageReturn
      accContent = '\r';
    }

    for (; j < result.length; j++) {
      const next = result[j]!;
      if (next.type === 'cursorMove') {
        accContent += cursorMove(next.x, next.y);
      } else if (next.type === 'carriageReturn') {
        accContent += '\r';
      } else if (next.type === 'styleStr') {
        styleBuf += next.str;
      } else if (next.type === 'stdout') {
        foundStdout = true;
        break;
      } else {
        // Any other patch type (clear, cursorHide, hyperlink, …)
        // blocks the merge — stop scanning.
        break;
      }
    }

    if (foundStdout) {
      // Combine accumulated cursor movement + styles + content into one
      // stdout patch so the terminal receives a single contiguous byte
      // run instead of alternating move / write escape sequences.
      const stdoutPatch = result[j]! as { type: 'stdout'; content: string };
      merged.push({
        type: 'stdout',
        content: accContent + styleBuf + stdoutPatch.content,
      });
      i = j + 1; // skip past the consumed stdout

      // Continue merging consecutive stdout patches after this one.
      while (i < result.length && result[i]!.type === 'stdout') {
        (merged[merged.length - 1]! as { content: string }).content += (
          result[i]! as { content: string }
        ).content;
        i++;
      }
    } else {
      // No stdout found — emit the original patch and retry.
      merged.push(patch);
      i++;
    }
  }

  return merged;
}
