import { HttpError, type Attachment, type DroppedRef, type KeptText } from "./port";
import { SseHttp } from "./sse_http";
import { SseTheme } from "./sse_theme";
import { guardUpload, tooLarge, UPLOAD_LIMIT_MB } from "./upload";
import { host } from "./host";

// Where a file enters a turn: bytes the page holds, or paths the shell knows.
export class SseIntake extends SseTheme {
  async attach(blob: Blob, name?: string, signal?: AbortSignal) {
    const image = !name || blob.type.toLowerCase().startsWith("image/");
    if (image) guardUpload(blob.size, UPLOAD_LIMIT_MB.image, tooLarge("attachment.too_large", "limit_mb"));
    if (!image && blob instanceof File) {
      const path = host().pathsForFiles([blob])[0];
      if (path) {
        const [ref] = await this.dropRefs([path]);
        if (!ref?.ref) throw new HttpError(0, ref?.error ?? "attachment is unreadable", { code: "attachment.unreadable", error: ref?.error });
        return { path: ref.path ?? path, ref: ref.ref, image: ref.image } as Attachment;
      }
    }
    const query = new URLSearchParams({ name: name ?? "", mime: blob.type });
    let res: Response;
    try {
      res = await fetch(this.base + "/attachments/upload?" + query, {
        method: "POST",
        headers: { "content-type": "application/octet-stream" },
        credentials: "same-origin",
        body: blob,
        signal,
      });
    } catch (e) {
      if (signal?.aborted) throw e;
      if (!(e instanceof DOMException) || e.name !== "NotReadableError") throw e;
      const detail = String(e);
      throw new HttpError(0, detail, { code: "attachment.unreadable", error: detail, params: { detail } });
    }
    if (!res.ok) await SseHttp.fail("/attachments", res);
    return (await res.json()) as Attachment;
  }

  async keepText(text: string) {
    const res = await fetch(this.base + "/attachments", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ kind: "text", text }),
    });
    if (!res.ok) await SseHttp.fail("/attachments", res);
    return (await res.json()) as KeptText;
  }

  dropRefs(paths: string[]) {
    return this.post0<DroppedRef[]>("/drop", { paths });
  }
}
