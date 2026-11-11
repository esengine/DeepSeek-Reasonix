import { readFileSync } from "node:fs";
import { gunzipSync } from "node:zlib";
import { describe, expect, it } from "vitest";

const ILLEGAL = /[<>:"|?*\u0000-\u001f]/;

function entryNames(tgz: Buffer): string[] {
  const tar = gunzipSync(tgz);
  const names: string[] = [];
  for (let at = 0; at + 512 <= tar.length; ) {
    const header = tar.subarray(at, at + 512);
    if (header.every((b) => b === 0)) break;
    const name = header.subarray(0, 100).toString("utf8").replace(/\0.*$/, "");
    const prefix = header.subarray(345, 500).toString("utf8").replace(/\0.*$/, "");
    const size = parseInt(header.subarray(124, 136).toString("utf8").replace(/\0.*$/, "").trim() || "0", 8);
    names.push(prefix ? `${prefix}/${name}` : name);
    at += 512 + Math.ceil(size / 512) * 512;
  }
  return names;
}

describe("a package vendored for installs on Windows", () => {
  const dir = new URL("../vendor/", import.meta.url);

  it("holds no file name Windows cannot create", () => {
    const names = entryNames(readFileSync(new URL("redos-detector-6.1.4.tgz", dir)));
    expect(names.length).toBeGreaterThan(10);
    expect(names.filter((n) => ILLEGAL.test(n))).toEqual([]);
  });

  it("is the package the override says it is", () => {
    const names = entryNames(readFileSync(new URL("redos-detector-6.1.4.tgz", dir)));
    expect(names).toContain("package/package.json");
    expect(names).toContain("package/LICENSE");
    expect(names).toContain("package/dist/redos-detector.js");
  });

  it("is the one package.json points the override at", () => {
    const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
    expect(pkg.pnpm.overrides["redos-detector"]).toBe("file:./vendor/redos-detector-6.1.4.tgz");
  });
});
