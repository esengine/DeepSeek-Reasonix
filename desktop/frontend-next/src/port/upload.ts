import { HttpError } from "./http_error";

// The kernel's caps on what a page may upload (serve/attachments.go,
// appearance.go, themes_install.go), in megabytes. Each upload is refused here,
// before the file is read: turning one into base64 is several copies of it in
// the renderer's memory, and the kernel would refuse it anyway. A Go test pins
// these numbers to the kernel's.
export const UPLOAD_LIMIT_MB = { attachment: 25, image: 10, scene: 32, theme: 48 } as const;

const MB = 1024 * 1024;

/** Throws the refusal `refuse` builds when `bytes` is past `limitMb`, before any read. */
export function guardUpload(bytes: number, limitMb: number, refuse: (limitMb: number) => Error): void {
  if (bytes > limitMb * MB) throw refuse(limitMb);
}

/** A typed refusal carrying the kernel's own code, so the localized message is the one the kernel's refusal gets. */
export function tooLarge(code: string, limitKey: "limit_mb" | "limit"): (limitMb: number) => HttpError {
  return (limit) => {
    const detail = `larger than ${limit} MB`;
    return new HttpError(413, detail, { code, error: detail, params: { [limitKey]: limit } });
  };
}

export async function base64Of(blob: Blob): Promise<string> {
  const buf = new Uint8Array(await blob.arrayBuffer());
  let bin = "";
  for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
  return btoa(bin);
}
