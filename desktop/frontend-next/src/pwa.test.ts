import { describe, expect, it, vi } from "vitest";

const fs = await vi.importActual<{ readFileSync: (path: string, encoding: string) => string; existsSync: (path: string) => boolean }>("node:fs");
const cp = await vi.importActual<{ spawnSync: (cmd: string, args: string[]) => { status: number | null } }>("node:child_process");
const read = (p: string) => fs.readFileSync(p, "utf8");
const manifest = JSON.parse(read("public/manifest.webmanifest"));
const ignored = (p: string) => cp.spawnSync("git", ["check-ignore", "-q", p]).status === 0;

describe("the phone page can be added to the home screen", () => {
  it("links the manifest and the touch icon from the page", () => {
    const html = read("index.html");
    expect(html).toMatch(/<link rel="manifest" href="[^"]*manifest\.webmanifest"/);
    expect(html).toMatch(/<link rel="apple-touch-icon" href="[^"]*apple-touch-icon\.png"/);
    expect(html).toMatch(/<meta name="theme-color" content="#07080c"/);
  });

  it("opens full screen at the page it was installed from", () => {
    expect(manifest.display).toBe("standalone");
    expect(manifest.start_url).toBe("./");
    expect(manifest.scope).toBe("./");
    expect(manifest.name).toBeTruthy();
  });

  it("ships the icon sizes an install needs", () => {
    const sizes = manifest.icons.map((i: { sizes: string }) => i.sizes);
    expect(sizes).toContain("192x192");
    expect(sizes).toContain("512x512");
    expect(manifest.icons.some((i: { purpose: string }) => i.purpose === "maskable")).toBe(true);
  });

  it("has every icon on disk and none of them ignored by git", () => {
    const files = [...manifest.icons.map((i: { src: string }) => `public/${i.src}`), "public/icons/apple-touch-icon.png", "public/icons/icon.svg"];
    for (const f of files) {
      expect(fs.existsSync(f), `${f} is missing`).toBe(true);
      expect(ignored(f), `${f} is ignored by git, so it would never reach a build`).toBe(false);
    }
  });

  it("registers no service worker, so no cached page can outlive a credential", () => {
    expect(read("index.html")).not.toMatch(/serviceWorker/);
  });
});
