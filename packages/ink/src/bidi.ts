// @ts-nocheck
/** Software-side bidirectional reordering for clustered terminal cells. */
import bidiFactory from 'bidi-js';

type ClusteredChar = {
  value: string;
  width: number;
  styleId: number;
  hyperlink: string | undefined;
};

let bidiInstance: ReturnType<typeof bidiFactory> | undefined;
let cachedNeedsBidi: boolean | undefined;

// Windows lacks terminal-side bidi entirely. Windows Terminal hosting WSL
// still routes output through conhost, so WSL processes inherit the same
// gap — we detect that via WT_SESSION rather than process.platform. VS Code's
// xterm.js doesn't implement bidi either and is reachable on any OS.
function needsSoftwareBidi(): boolean {
  if (cachedNeedsBidi === undefined) {
    cachedNeedsBidi =
      process.platform === 'win32' ||
      typeof process.env['WT_SESSION'] === 'string' ||
      process.env['TERM_PROGRAM'] === 'vscode';
  }
  return cachedNeedsBidi;
}

function getBidi() {
  if (!bidiInstance) {
    bidiInstance = bidiFactory();
  }
  return bidiInstance;
}

export function reorderBidi(characters: ClusteredChar[]): ClusteredChar[] {
  if (!needsSoftwareBidi() || characters.length === 0) {
    return characters;
  }

  const plainText = characters.map(c => c.value).join('');

  // Probing for RTL code points first lets us skip the bidi-js call entirely
  // on the dominant LTR-only path — paragraph-level scan with one regex test.
  if (!hasRtlCharacters(plainText)) {
    return characters;
  }

  const bidi = getBidi();
  const { levels } = bidi.getEmbeddingLevels(plainText, 'auto');

  // bidi-js returns one level per UTF-16 code unit. Walk the cluster array
  // in parallel, taking the level of the first code unit of each cluster as
  // the cluster's level — within a cluster all units share a level.
  const charLevels: number[] = [];
  let offset = 0;
  for (let i = 0; i < characters.length; i++) {
    charLevels.push(levels[offset]!);
    offset += characters[i]!.value.length;
  }

  // Classic UBA reorder: from the deepest embedding level down to 1, reverse
  // each maximal run of clusters whose level is >= the current level. The
  // result is identical to bidi-js's reorderSegments output but expressed
  // over cluster indices so we don't have to map back from string offsets.
  const reordered = [...characters];
  const maxLevel = Math.max(...charLevels);

  for (let level = maxLevel; level >= 1; level--) {
    let i = 0;
    while (i < reordered.length) {
      if (charLevels[i]! >= level) {
        let j = i + 1;
        while (j < reordered.length && charLevels[j]! >= level) {
          j++;
        }
        reverseRange(reordered, i, j - 1);
        reverseRange(charLevels, i, j - 1);
        i = j;
      } else {
        i++;
      }
    }
  }

  return reordered;
}

function reverseRange<T>(arr: T[], start: number, end: number): void {
  while (start < end) {
    const tmp = arr[start]!;
    arr[start] = arr[end]!;
    arr[end] = tmp;
    start++;
    end--;
  }
}

// Conservative pre-check for any code point that could carry an RTL bidi
// class. We err on the side of false positives (running the full algorithm
// on a string that turns out to be neutral is harmless) and avoid false
// negatives that would leave RTL text visibly reversed.
//
// Covers: Hebrew (incl. presentation forms), Arabic (incl. supplement,
// extended-A, presentation forms A/B), Thaana, and Syriac.
function hasRtlCharacters(text: string): boolean {
  return /[\u0590-\u05FF\uFB1D-\uFB4F\u0600-\u06FF\u0750-\u077F\u08A0-\u08FF\uFB50-\uFDFF\uFE70-\uFEFF\u0780-\u07BF\u0700-\u074F]/u.test(
    text,
  );
}
