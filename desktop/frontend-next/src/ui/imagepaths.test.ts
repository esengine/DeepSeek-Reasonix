import { describe, expect, it } from "vitest";
import { annotateImagePath, parseImagePaths } from "./imagepaths";

describe("annotateImagePath / parseImagePaths round-trips", () => {
  it("working-folder and drive paths keep their spelling", () => {
    expect(annotateImagePath("a.jpg")).toBe("@//a.jpg/");
    expect(annotateImagePath("c:\\a.jpg")).toBe("@//c/:\\a.jpg");
    expect(parseImagePaths("@//a.jpg/ 请看").paths).toEqual(["a.jpg"]);
    expect(parseImagePaths("@//c/:\\a.jpg 请看").paths).toEqual(["c:\\a.jpg"]);
  });

  it("POSIX absolute paths keep their leading root", () => {
    expect(annotateImagePath("/folder/outside.png")).toBe("@///folder/outside.png");
    expect(parseImagePaths("@///folder/outside.png 请看").paths).toEqual(["/folder/outside.png"]);
    expect(parseImagePaths("@///folder/outside.png 请看").text).toBe("请看");
  });

  it("reads every path out and leaves the words behind", () => {
    const out = parseImagePaths("@//a.jpg/ @//c/:\\b.png look");
    expect(out.paths).toEqual(["a.jpg", "c:\\b.png"]);
    expect(out.text).toBe("look");
  });
});
