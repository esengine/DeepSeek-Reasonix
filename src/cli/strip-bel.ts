// Windows cmd.exe rings the system bell when stdout carries a bare BEL byte,
// which third-party libs (ansi-escapes, terminal-link, …) embed as OSC
// terminators. Swap BEL → ST (`\x1b\\`) on Windows: ST is the other valid
// OSC terminator so semantics are preserved for legitimate OSC sequences,
// and stray bells outside OSC stop beeping. Issue #1786.

if (process.platform === "win32") {
  // biome-ignore lint/suspicious/noControlCharactersInRegex: matching BEL is the point
  const BEL_RE = /\x07/g;
  const replaceBel = (chunk: unknown): unknown => {
    if (typeof chunk === "string") {
      return chunk.includes("\x07") ? chunk.replace(BEL_RE, "\x1b\\") : chunk;
    }
    if (Buffer.isBuffer(chunk)) {
      if (chunk.indexOf(0x07) === -1) return chunk;
      // Each BEL grows by 1 byte (\x07 → \x1b\\), bound the output up front.
      const out = Buffer.alloc(chunk.length * 2);
      let j = 0;
      for (let i = 0; i < chunk.length; i++) {
        if (chunk[i] === 0x07) {
          out[j++] = 0x1b;
          out[j++] = 0x5c;
        } else {
          out[j++] = chunk[i]!;
        }
      }
      return out.subarray(0, j);
    }
    return chunk;
  };

  for (const stream of [process.stdout, process.stderr]) {
    const original = stream.write.bind(stream) as (...args: unknown[]) => boolean;
    stream.write = ((chunk: unknown, ...args: unknown[]) =>
      original(replaceBel(chunk), ...args)) as typeof stream.write;
  }
}
