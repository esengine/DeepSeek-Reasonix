import { afterEach, describe, expect, it, vi } from "vitest";
import { HttpError } from "./port";
import { SsePort } from "./sse";
import { themeBodyBytes } from "./sse_theme";

afterEach(() => vi.unstubAllGlobals());

const sized = (mb: number, read = vi.fn(async () => new ArrayBuffer(0))) =>
  ({ type: "image/png", name: "big.bin", size: mb * 1024 * 1024, arrayBuffer: read }) as unknown as File;

function noNetwork() {
  const fetched = vi.fn();
  vi.stubGlobal("fetch", fetched);
  return fetched;
}

describe("an upload past the kernel's cap is refused before it is read", () => {
  it("image attachments, with the kernel's own code", async () => {
    const fetched = noNetwork();
    const read = vi.fn(async () => new ArrayBuffer(0));
    const err = await new SsePort("", "r1").attach(sized(600, read), "shot.png").catch((e) => e);
    expect(err).toBeInstanceOf(HttpError);
    expect(err.reason).toMatchObject({ code: "attachment.too_large", params: { limit_mb: 10 } });
    expect(read).not.toHaveBeenCalled();
    expect(fetched).not.toHaveBeenCalled();
  });

  it("pictures against the kernel's stricter image cap, a file against the general one", async () => {
    const fetched = noNetwork();
    const read = vi.fn(async () => new ArrayBuffer(0));
    const img = Object.assign(sized(15, read), { type: "image/png" });
    const err = await new SsePort("", "r1").attach(img, "shot.png").catch((e) => e);
    expect(err.reason).toMatchObject({ code: "attachment.too_large", params: { limit_mb: 10 } });
    expect(read).not.toHaveBeenCalled();
    const zip = Object.assign(sized(15, read), { type: "application/zip" });
    vi.stubGlobal("fetch", async () => ({ ok: true, status: 200, json: async () => ({ path: "p", ref: "@p", image: false }) }));
    await new SsePort("", "r1").attach(zip, "pack.zip");
    expect(read).not.toHaveBeenCalled();
    expect(fetched).not.toHaveBeenCalled();
  });

  it("wallpapers, with the kernel's own code", async () => {
    const fetched = noNetwork();
    const read = vi.fn(async () => new ArrayBuffer(0));
    const err = await new SsePort("", "r1").uploadWallpaper(sized(200, read)).catch((e) => e);
    expect(err.reason).toMatchObject({ code: "wallpaper.too_large", params: { limit: 12 } });
    expect(read).not.toHaveBeenCalled();
    expect(fetched).not.toHaveBeenCalled();
  });

  it("theme packs, counting every file picked", async () => {
    const fetched = noNetwork();
    const read = vi.fn(async () => new ArrayBuffer(0));
    const files = [sized(20, read), sized(20, read)];
    files[0] = Object.assign(files[0], { name: "theme.json" });
    files[1] = Object.assign(files[1], { name: "bg.png" });
    const err = await new SsePort("", "r1").importTheme(files).catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.message).toBe("主题包过大，请控制在 48 MB 以内");
    expect(read).not.toHaveBeenCalled();
    expect(fetched).not.toHaveBeenCalled();
  });

  it("lets a file under the cap through to the kernel", async () => {
    vi.stubGlobal("fetch", async () => ({ ok: true, status: 200, json: async () => ({ path: "p", ref: "@p", image: false }) }));
    expect(await new SsePort("", "r1").attach(new Blob(["x"]), "a.txt")).toEqual({ path: "p", ref: "@p", image: false });
  });
});

describe("the theme body estimate is the body the page will send", () => {
  it("matches the real JSON for loose files, with padding and names", () => {
    const files = [{ name: "theme.json", size: 1 }, { name: "背景.png", size: 1000 }, { name: "a\"b.png", size: 2 }];
    const body = JSON.stringify({ files: Object.fromEntries(files.map((f) => [f.name, "x".repeat(Math.ceil(f.size / 3) * 4)])) });
    expect(themeBodyBytes(files)).toBe(new TextEncoder().encode(body).length);
  });

  it("matches the real JSON for one archive", () => {
    const body = JSON.stringify({ name: "pack.zip", zip: "x".repeat(Math.ceil(5000 / 3) * 4) });
    expect(themeBodyBytes([{ name: "pack.zip", size: 5000 }])).toBe(body.length);
  });

  it("refuses at the edge: a pack whose body is just past the cap is refused, just under is not", async () => {
    const cap = 48 * 1024 * 1024;
    const under = Math.floor(((cap - 80) / 4) * 3);
    const mk = (n: number) => ({ name: "pack.zip", size: n });
    expect(themeBodyBytes([mk(under)])).toBeLessThanOrEqual(cap);
    expect(themeBodyBytes([mk(under + 3 * 64)])).toBeGreaterThan(cap);
  });
});
