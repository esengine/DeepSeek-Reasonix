import { describe, expect, it } from "vitest";
import { tildeify } from "../src/tildeify.js";

describe("tildeify", () => {
  it("returns ~ for the home directory itself", () => {
    const home = process.env.HOME ?? process.env.USERPROFILE ?? "/home/test";
    expect(tildeify(home)).toBe("~");
  });

  it("replaces home prefix with ~", () => {
    const home = process.env.HOME ?? process.env.USERPROFILE ?? "/home/test";
    expect(tildeify(`${home}/projects/foo`)).toBe("~/projects/foo");
  });

  it("leaves unrelated paths untouched", () => {
    expect(tildeify("/usr/local/bin")).toBe("/usr/local/bin");
    expect(tildeify("C:\\Windows")).toBe("C:\\Windows");
  });

  it("handles trailing slashes on home", () => {
    const home = process.env.HOME ?? process.env.USERPROFILE ?? "/home/test";
    expect(tildeify(`${home}//projects`)).toBe("~/projects");
  });
});
