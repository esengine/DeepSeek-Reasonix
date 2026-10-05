import { describe, expect, it } from "vitest";
import { admit, megabytes } from "./feedbackshots";

const limits = { images: 2, uploadBytes: 100 };
const file = (name: string, type: string, size: number) => new File([new Uint8Array(size)], name, { type });

describe("which files become screenshots", () => {
  it("takes PNG and JPEG and refuses the rest by type", () => {
    const { taken, refused } = admit(0, [file("a.png", "image/png", 1), file("b.jpg", "image/jpeg", 1), file("c.gif", "image/gif", 1), file("d.pdf", "application/pdf", 1)], { images: 9, uploadBytes: 100 });
    expect(taken.map((f) => f.name)).toEqual(["a.png", "b.jpg"]);
    expect(refused).toEqual([{ name: "c.gif", why: "format" }, { name: "d.pdf", why: "format" }]);
  });

  it("refuses a file past the upload size without spending a slot on it", () => {
    const { taken, refused } = admit(0, [file("big.png", "image/png", 101), file("ok.png", "image/png", 100)], limits);
    expect(taken.map((f) => f.name)).toEqual(["ok.png"]);
    expect(refused).toEqual([{ name: "big.png", why: "too_large" }]);
  });

  it("counts what is already held against the limit", () => {
    const { taken, refused } = admit(1, [file("a.png", "image/png", 1), file("b.png", "image/png", 1)], limits);
    expect(taken.map((f) => f.name)).toEqual(["a.png"]);
    expect(refused).toEqual([{ name: "b.png", why: "too_many" }]);
  });

  it("prints sizes in whole or tenth megabytes", () => {
    expect(megabytes(10 << 20)).toBe("10");
    expect(megabytes(1_572_864)).toBe("1.5");
  });
});
