const MAX_TERMINAL_CHAT_CHARS = 120_000;

function consumeCSI(value: string, start: number): number {
  let index = start;
  while (index < value.length) {
    const code = value.charCodeAt(index);
    index += 1;
    if (code >= 0x40 && code <= 0x7e) return index;
  }
  return value.length;
}

function consumeStringSequence(value: string, start: number): number {
  let index = start;
  while (index < value.length) {
    const code = value.charCodeAt(index);
    if (code === 0x07 || code === 0x9c) return index + 1;
    if (code === 0x1b && value.charCodeAt(index + 1) === 0x5c) return index + 2;
    index += 1;
  }
  return value.length;
}

function consumeEscapeSequence(value: string, start: number): number {
  const first = value.charCodeAt(start + 1);
  if (first === 0x5b) return consumeCSI(value, start + 2);
  if (first === 0x5d || first === 0x50 || first === 0x5e || first === 0x5f || first === 0x58) {
    return consumeStringSequence(value, start + 2);
  }
  let index = start + 1;
  while (index < value.length) {
    const code = value.charCodeAt(index);
    index += 1;
    if (code >= 0x30 && code <= 0x7e) return index;
    if (code < 0x20 || code > 0x2f) return index;
  }
  return value.length;
}

function consumeC1Sequence(value: string, start: number): number {
  const code = value.charCodeAt(start);
  if (code === 0x9b) {
    return consumeCSI(value, start + 1);
  }
  if (code === 0x90 || code === 0x98 || code === 0x9d || code === 0x9e || code === 0x9f) {
    return consumeStringSequence(value, start + 1);
  }
  return start + 1;
}

export function stripTerminalControlSequences(value: string): string {
  let result = "";
  for (let index = 0; index < value.length;) {
    const code = value.charCodeAt(index);
    if (code === 0x1b) {
      index = consumeEscapeSequence(value, index);
      continue;
    }
    if (code >= 0x80 && code <= 0x9f) {
      index = consumeC1Sequence(value, index);
      continue;
    }
    if (code < 0x20 && code !== 0x09 && code !== 0x0a && code !== 0x0d) {
      index += 1;
      continue;
    }
    if (code === 0x7f) {
      index += 1;
      continue;
    }
    result += value[index];
    index += 1;
  }
  return result.replace(/\r\n?/g, "\n");
}

function markdownFenceFor(value: string): string {
  let longestRun = 0;
  let currentRun = 0;
  for (const character of value) {
    if (character === "`") {
      currentRun += 1;
      longestRun = Math.max(longestRun, currentRun);
    } else {
      currentRun = 0;
    }
  }
  return "`".repeat(Math.max(3, longestRun + 1));
}

export function formatTerminalOutputForComposer(value: string, maxChars = MAX_TERMINAL_CHAT_CHARS): string {
  const normalized = stripTerminalControlSequences(value);
  const clipped = normalized.length > maxChars ? normalized.slice(-maxChars) : normalized;
  if (!clipped.trim()) return "";
  const fence = markdownFenceFor(clipped);
  return `${fence}console\n${clipped}\n${fence}`;
}
