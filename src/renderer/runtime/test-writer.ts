export interface TestWriter {
  readonly write: (bytes: string) => void;
  output(): string;
  flush(): string;
  chunks(): ReadonlyArray<string>;
}

export function makeTestWriter(): TestWriter {
  const buf: string[] = [];
  return {
    write: (bytes: string) => {
      buf.push(bytes);
    },
    output: () => buf.join(""),
    flush: () => {
      const out = buf.join("");
      buf.length = 0;
      return out;
    },
    chunks: () => buf.slice(),
  };
}
