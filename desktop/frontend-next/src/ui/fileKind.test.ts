import { expect, it } from "vitest";
import { fileKind } from "./fileKind";

it("sorts a path by its extension into one of a few kinds", () => {
  expect(fileKind("src/App.tsx")).toBe("code");
  expect(fileKind("main.go")).toBe("code");
  expect(fileKind("README.md")).toBe("doc");
  expect(fileKind("config/app.JSON")).toBe("data");
  expect(fileKind("a/b/logo.png")).toBe("image");
});

it("leaves an unknown or missing extension plain", () => {
  expect(fileKind("Makefile")).toBe("");
  expect(fileKind("dir.d/noext")).toBe("");
  expect(fileKind("x.unknownext")).toBe("");
});
