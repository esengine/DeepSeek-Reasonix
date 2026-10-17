import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpError } from "./port";
import { SsePort } from "./sse";
import { host } from "./host";

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const refusal = (status: number, body: unknown) =>
  vi.stubGlobal("fetch", async () => ({ ok: false, status, text: async () => JSON.stringify(body) }));

describe("attach", () => {
  it("keeps the kernel's refusal code and params on the error it throws", async () => {
    refusal(415, { code: "attachment.unsupported_image", error: "pasted data is not a supported image", params: { format: ".ico" } });
    const err = await new SsePort("", "r1").attach(new Blob(["x"], { type: "image/x-icon" }), "a.ico").catch((e) => e);
    expect(err).toBeInstanceOf(HttpError);
    expect(err.reason).toMatchObject({ code: "attachment.unsupported_image", params: { format: ".ico" } });
  });

  it("falls back to the plain text of an older kernel", async () => {
    vi.stubGlobal("fetch", async () => ({ ok: false, status: 400, text: async () => "pasted data is not a supported image" }));
    const err = await new SsePort("", "r1").attach(new Blob(["x"]), "a.png").catch((e) => e);
    expect(err.message).toBe("pasted data is not a supported image");
  });

  it("names an unreadable file instead of passing the browser's English through", async () => {
    vi.stubGlobal("fetch", async () => { throw new DOMException("denied", "NotReadableError"); });
    const blob = { type: "", arrayBuffer: async () => Promise.reject(new DOMException("denied", "NotReadableError")) } as unknown as Blob;
    const err = await new SsePort("", "r1").attach(blob, "locked.docx").catch((e) => e);
    expect(err).toBeInstanceOf(HttpError);
    expect(err.reason?.code).toBe("attachment.unreadable");
  });

  it("sends a large APK as the original Blob without reading it into renderer memory", async () => {
    const read = vi.fn(async () => new ArrayBuffer(0));
    const fetched = vi.fn(async () => ({ ok: true, json: async () => ({ path: "p", ref: "@p", image: false }) }));
    vi.stubGlobal("fetch", fetched);
    const huge = { type: "application/zip", size: 600 * 1024 * 1024, arrayBuffer: read } as unknown as Blob;
    expect(await new SsePort("", "r1").attach(huge, "large.apk")).toMatchObject({ ref: "@p" });
    expect(read).not.toHaveBeenCalled();
    expect(fetched).toHaveBeenCalledWith(expect.stringContaining("/attachments/upload?"), expect.objectContaining({ body: huge, headers: { "content-type": "application/octet-stream" } }));
  });

  it("uses a native file reference before reading or uploading selected files", async () => {
    vi.stubGlobal("window", {});
    vi.spyOn(host(), "pathsForFiles").mockReturnValue(["/tmp/large.apk"]);
    const fetched = vi.fn(async () => ({ ok: true, json: async () => [{ path: "/tmp/large.apk", ref: "@selected/large.apk", image: false }] }));
    vi.stubGlobal("fetch", fetched);
    const file = new File(["apk"], "large.apk", { type: "application/vnd.android.package-archive" });
    const read = vi.spyOn(file, "arrayBuffer");
    expect(await new SsePort("", "r1").attach(file, file.name)).toMatchObject({ ref: "@selected/large.apk" });
    expect(fetched).toHaveBeenCalledOnce();
    expect(fetched).toHaveBeenCalledWith("/drop", expect.objectContaining({ body: JSON.stringify({ paths: ["/tmp/large.apk"] }) }));
    expect(read).not.toHaveBeenCalled();
  });

  it("passes cancellation to the binary request", async () => {
    const ctl = new AbortController();
    ctl.abort();
    const failure = new DOMException("canceled", "AbortError");
    const fetched = vi.fn(async () => { throw failure; });
    vi.stubGlobal("fetch", fetched);
    await expect(new SsePort("", "r1").attach(new Blob(["apk"]), "app.apk", ctl.signal)).rejects.toBe(failure);
    expect(fetched).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ signal: ctl.signal }));
  });

  it("preserves transport refusals such as the remote relay size cap", async () => {
    const failure = new Error("remote upload is larger than 16 MB");
    vi.stubGlobal("fetch", async () => { throw failure; });
    await expect(new SsePort("", "r1").attach(new Blob(["apk"]), "app.apk")).rejects.toBe(failure);
  });

  it("still returns the saved reference on success", async () => {
    vi.stubGlobal("fetch", async () => ({ ok: true, status: 200, json: async () => ({ path: "p", ref: "@p", image: false }) }));
    expect(await new SsePort("", "r1").attach(new Blob(["x"]), "a.txt")).toEqual({ path: "p", ref: "@p", image: false });
  });
});
