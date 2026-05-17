import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { type ResolverIO, resolveRendererWith } from "../src/cli/ui/scene/renderer-resolver.js";

type IOOverrides = Partial<ResolverIO>;

function makeIO(overrides: IOOverrides = {}): ResolverIO {
  return {
    envCmd: () => undefined,
    envBin: () => undefined,
    hasFile: () => false,
    resolveOptionalDep: () => undefined,
    findReasonixSourceTree: () => undefined,
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

  it("prefers source-tree target/release over cargo run", () => {
    const releasePath = join("/repo", "target", "release", "reasonix-render");
    const r = resolveRendererWith(
      makeIO({
        findReasonixSourceTree: () => "/repo",
        hasCargo: () => true,
        hasFile: (p) => p === releasePath,
      }),
    );
    expect(r.source).toBe("prebuilt-release");
    expect(r.command).toEqual([releasePath]);
    expect(r.inputCommand).toEqual([releasePath, "--emit-input"]);
  });

  it("falls back to source-tree target/debug when release is absent", () => {
    const debugPath = join("/repo", "target", "debug", "reasonix-render");
    const r = resolveRendererWith(
      makeIO({
        findReasonixSourceTree: () => "/repo",
        hasCargo: () => true,
        hasFile: (p) => p === debugPath,
      }),
    );
    expect(r.source).toBe("prebuilt-debug");
    expect(r.command).toEqual([debugPath]);
  });

  it("falls back to cargo run only when no prebuilt binary exists", () => {
    const r = resolveRendererWith(
      makeIO({
        findReasonixSourceTree: () => "/repo",
        hasCargo: () => true,
        hasFile: () => false,
      }),
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

  it("requires source tree for cargo fallback", () => {
    expect(
      resolveRendererWith(makeIO({ findReasonixSourceTree: () => undefined, hasCargo: () => true }))
        .source,
    ).toBeNull();
  });

  it("requires cargo for cargo fallback", () => {
    expect(
      resolveRendererWith(
        makeIO({
          findReasonixSourceTree: () => "/repo",
          hasCargo: () => false,
          hasFile: () => false,
        }),
      ).source,
    ).toBeNull();
  });

  it("uses .exe suffix when platform is win32", () => {
    let probed: string | undefined;
    const r = resolveRendererWith(
      makeIO({
        platform: "win32",
        findReasonixSourceTree: () => "/repo",
        hasFile: (p) => {
          probed = probed ?? p;
          return p.endsWith("reasonix-render.exe");
        },
      }),
    );
    expect(r.source).toBe("prebuilt-release");
    expect(r.command[0]).toMatch(/reasonix-render\.exe$/);
    expect(probed).toMatch(/reasonix-render\.exe$/);
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
