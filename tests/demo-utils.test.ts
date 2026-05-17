import { describe, expect, it } from "vitest";
import { add, greet } from "../src/demo-utils.js";

describe("greet", () => {
  it("returns a formatted greeting", () => {
    expect(greet("Alice")).toBe("Hello, Alice! Welcome to Reasonix Code.");
  });
});

describe("add", () => {
  it("adds two positive numbers", () => {
    expect(add(1, 2)).toBe(3);
  });

  it("handles negative numbers", () => {
    expect(add(-1, 1)).toBe(0);
  });
});
