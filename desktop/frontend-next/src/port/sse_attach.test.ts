import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpError } from "./port";
import { SsePort } from "./sse";

afterEach(() => vi.unstubAllGlobals());

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
    vi.stubGlobal("fetch", async () => {
      throw new Error("must not be reached");
    });
    const blob = { type: "", arrayBuffer: async () => Promise.reject(new DOMException("denied", "NotReadableError")) } as unknown as Blob;
    const err = await new SsePort("", "r1").attach(blob, "locked.docx").catch((e) => e);
    expect(err).toBeInstanceOf(HttpError);
    expect(err.reason?.code).toBe("attachment.unreadable");
  });

  it("still returns the saved reference on success", async () => {
    vi.stubGlobal("fetch", async () => ({ ok: true, status: 200, json: async () => ({ path: "p", ref: "@p", image: false }) }));
    expect(await new SsePort("", "r1").attach(new Blob(["x"]), "a.txt")).toEqual({ path: "p", ref: "@p", image: false });
  });
});
