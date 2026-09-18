export interface SessionAttachmentChunk {
  data?: string;
  nextOffset: number;
  done: boolean;
}

const previewBudgetBytes = 64 << 20;

export async function readSessionAttachmentDataURL(
  tabId: string,
  digest: string,
  mime: string,
  read: (tabId: string, digest: string, offset: number) => Promise<SessionAttachmentChunk>,
): Promise<string> {
  const chunks: Uint8Array[] = [];
  let offset = 0;
  let total = 0;
  for (;;) {
    const chunk = await read(tabId, digest, offset);
    if (chunk.data) {
      const bytes = Uint8Array.from(atob(chunk.data), (c) => c.charCodeAt(0));
      total += bytes.length;
      if (total > previewBudgetBytes) {
        throw new Error("attachment preview exceeds preview budget");
      }
      chunks.push(bytes);
    }
    if (chunk.done) break;
    if (!Number.isFinite(chunk.nextOffset) || chunk.nextOffset <= offset) {
      throw new Error("attachment read did not advance");
    }
    offset = chunk.nextOffset;
  }
  const merged = new Uint8Array(total);
  let cursor = 0;
  for (const part of chunks) {
    merged.set(part, cursor);
    cursor += part.length;
  }
  const binary: string[] = [];
  for (let i = 0; i < merged.length; i += 32768) binary.push(String.fromCharCode(...merged.subarray(i, i + 32768)));
  return `data:${mime || "image/png"};base64,${btoa(binary.join(""))}`;
}
