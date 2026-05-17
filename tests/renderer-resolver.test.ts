import { describe, expect, it } from "vitest";
import { type ResolverIO, resolveRendererWith } from "../src/cli/ui/scene/renderer-resolver.js";

type IOOverrides = Partial<ResolverIO>;

function makeIO(overrides: IOOverrides = {}): ResolverIO {
  return {
    envCmd: () => undefined,
    envBin: () => undefined,
    hasFile: () => false,
    resolveOptionalDep: () => undefined,
    hasReasonixSourceTree: () => false,
    hasCargo: () => false,
    platform: "linux",
    ...overrides,
  };
}

describe("resolveRendererWith", () => {
  it("returns source=null when nothing is available", () => {
    const r = resolveRendererWith(makeIO());
    expect(r.source).toBeNull();
    expect(r.command).toEqual([]);
    expect(r.inputCommand).toEqual([]);
  });

  it("picks the optional-dep binary when present", () => {
    const r = resolveRendererWith(
      makeIO({
        resolveOptionalDep: () => "/opt/render/bin/reasonix-render",
      }),
    );
    expect(r.source).toBe("optional-dep");
    expect(r.command).toEqual(["/opt/render/bin/reasonix-render"]);
    expect(r.inputCommand).toEqual(["/opt/render/bin/reasonix-render", "--emit-input"]);
  });

  it("prefers env-bin over optional-dep", () => {
    const r = resolveRendererWith(
      makeIO({
        envBin: () => "/usr/local/bin/reasonix-render",
        hasFile: (p) => p === "/usr/local/bin/reasonix-render",
        resolveOptionalDep: () => "/opt/render/bin/reasonix-render",
      }),
    );
    expect(r.source).toBe("env-bin");
    expect(r.command).toEqual(["/usr/local/bin/reasonix-render"]);
  });

  it("ignores env-bin when the file does not exist", () => {
    const r = resolveRendererWith(
      makeIO({
        envBin: () => "/usr/local/bin/missing",
        hasFile: () => false,
        resolveOptionalDep: () => "/opt/render/bin/reasonix-render",
      }),
    );
    expect(r.source).toBe("optional-dep");
  });

  it("falls back to cargo only when source tree AND cargo are both present", () => {
    expect(
      resolveRendererWith(makeIO({ hasReasonixSourceTree: () => true, hasCargo: () => false }))
        .source,
    ).toBeNull();
    expect(
      resolveRendererWith(makeIO({ hasReasonixSourceTree: () => false, hasCargo: () => true }))
        .source,
    ).toBeNull();
    const r = resolveRendererWith(
      makeIO({ hasReasonixSourceTree: () => true, hasCargo: () => true }),
    );
    expect(r.source).toBe("cargo");
    expect(r.command).toEqual(["cargo", "run", "--quiet", "--bin", "reasonix-render"]);
    expect(r.inputCommand).toEqual([
      "cargo",
      "run",
      "--quiet",
      "--bin",
      "reasonix-render",
      "--",
      "--emit-input",
    ]);
  });

  it("uses .exe suffix correctly via the optional-dep callback", () => {
    const r = resolveRendererWith(
      makeIO({
        platform: "win32",
        resolveOptionalDep: (platform) => {
          expect(platform).toBe("win32");
          return "C:/x/reasonix-render.exe";
        },
      }),
    );
    expect(r.command).toEqual(["C:/x/reasonix-render.exe"]);
  });

  it("REASONIX_RENDER_CMD overrides only the renderer channel; input falls back to base", () => {
    const r = resolveRendererWith(
      makeIO({
        envCmd: (name) =>
          name === "REASONIX_RENDER_CMD" ? ["custom-render", "--flag"] : undefined,
        resolveOptionalDep: () => "/opt/bin/reasonix-render",
      }),
    );
    expect(r.command).toEqual(["custom-render", "--flag"]);
    expect(r.inputCommand).toEqual(["/opt/bin/reasonix-render", "--emit-input"]);
    expect(r.source).toBe("optional-dep");
  });

  it("REASONIX_INPUT_CMD overrides only the input channel", () => {
    const r = resolveRendererWith(
      makeIO({
        envCmd: (name) => (name === "REASONIX_INPUT_CMD" ? ["custom-input", "--flag"] : undefined),
        resolveOptionalDep: () => "/opt/bin/reasonix-render",
      }),
    );
    expect(r.command).toEqual(["/opt/bin/reasonix-render"]);
    expect(r.inputCommand).toEqual(["custom-input", "--flag"]);
  });

  it("legacy env vars work even with no base (source becomes env-cmd)", () => {
    const r = resolveRendererWith(
      makeIO({
        envCmd: (name) =>
          name === "REASONIX_RENDER_CMD"
            ? ["render"]
            : name === "REASONIX_INPUT_CMD"
              ? ["input"]
              : undefined,
      }),
    );
    expect(r.command).toEqual(["render"]);
    expect(r.inputCommand).toEqual(["input"]);
    expect(r.source).toBe("env-cmd");
  });
});
