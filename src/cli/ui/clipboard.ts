/** OSC 52 clipboard write + temp-file fallback. */

import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const OSC_52_LIMIT = 75_000;

export interface ClipboardWrite {
  osc52: boolean;
  filePath: string | null;
  size: number;
}

export function writeClipboard(text: string): ClipboardWrite {
  // mkdtemp creates a private 0700 directory atomically — keeps the
  // file out of the shared tmp namespace where a sibling process can
  // race or read it (CodeQL js/insecure-temporary-file).
  const dir = mkdtempSync(join(tmpdir(), "reasonix-clip-"));
  const filePath = join(dir, "clip.txt");
  let osc52 = false;
  if (text.length <= OSC_52_LIMIT) {
    const b64 = Buffer.from(text, "utf8").toString("base64");
    process.stdout.write(`\x1b]52;c;${b64}\x1b\\`);
    osc52 = true;
  }
  let writtenPath: string | null = null;
  try {
    writeFileSync(filePath, text, "utf8");
    writtenPath = filePath;
  } catch {
    /* read-only fs */
  }
  return { osc52, filePath: writtenPath, size: text.length };
}
