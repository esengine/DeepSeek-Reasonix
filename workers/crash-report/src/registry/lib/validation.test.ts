import { describe, expect, it } from "vitest";
import { ListQuerySchema, PublishSchema } from "./validation";

const base = { kind: "skill", name: "demo", source: "https://example.com/SKILL.md" };
const parse = (overrides: Record<string, unknown>) => PublishSchema.safeParse({ ...base, ...overrides });

describe("PublishSchema source", () => {
  it("accepts URLs, git: shorthands, and package names install_source can resolve", () => {
    // kind=mcp so the skill-only whole-repo guard never masks a shape accept.
    for (const source of [
      "https://github.com/o/r/blob/main/SKILL.md",
      "http://example.com/x/.mcp.json",
      "https://github.com/o/r/tree/main/skills/foo",
      "git:github.com/o/r",
      "@scope/pkg",
      "my-mcp-server",
    ]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(true);
    }
  });

  it("accepts an exact semver pin on a package source", () => {
    for (const source of [
      "@playwright/mcp@0.0.83",
      "name@1.2.3",
      "@scope/name@10.20.30",
      "name@1.2.3-beta.1",
      "name@1.2.3+build.5",
      "name@1.2.3-alpha.1+sha.abc",
    ]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(true);
    }
  });

  it("rejects ranges, dist-tags, empty versions and anything that is not a plain package spec", () => {
    for (const source of [
      "name@",
      "@scope/name@",
      "name@latest",
      "name@next",
      "name@^1.2.3",
      "name@~1.2.3",
      "name@>=1.2.3",
      "name@1",
      "name@1.2",
      "name@1.2.x",
      "name@*",
      "name@01.2.3",
      "name@1.2.3-",
      "name@v1.2.3",
      "a@b@c",
      "name@1.2.3@4.5.6",
      "@scope@1.2.3",
      "@scope/name/extra@1.2.3",
      "@1.2.3",
      "name@1.2.3;rm",
      "name@1.2.3 --foo",
      "name @1.2.3",
      "name@1.2.3/../x",
      "name@..",
      "../name@1.2.3",
      "/abs/name@1.2.3",
      "name@1.2.3\\x",
      "name@1.2.3$(id)",
      "name@1.2.3&x",
      "name@1.2.3|x",
      "file:name@1.2.3",
      "git+https://github.com/o/r.git@1.2.3",
      "npm:name@1.2.3",
      "o/r@1.2.3",
      "-y@1.2.3",
      "--help",
      "-y",
    ]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(false);
    }
  });

  it("rejects package scopes and names made only of dots", () => {
    for (const source of ["@a/..", "@../b", "@./b", "@a/.", "@../..", "@a/..@1.2.3"]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(false);
    }
    for (const source of ["@a/b.c", "@a/..b", "@a/b.."]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(true);
    }
  });

  it("still refuses a pinned package for kind=skill", () => {
    expect(parse({ kind: "skill", source: "@scope/pkg@1.2.3" }).success).toBe(false);
  });

  it("rejects free text, bare local paths, and scheme-less hosts", () => {
    for (const source of [
      "这个来源是自己制作的",
      "just some words",
      "./skills/foo",
      "/home/me/skill",
      "C:\\Users\\me\\skill",
      "github.com/o/r",
      "git:github.com/",
    ]) {
      expect(parse({ source }).success, source).toBe(false);
    }
  });

  it("keeps the kind=skill whole-repo guard (installs every skill in the repo)", () => {
    expect(parse({ kind: "skill", source: "https://github.com/o/r" }).success).toBe(false);
  });

  it("allows a whole-repo GitHub source for kind=mcp (a repo root is a valid plugin/MCP source)", () => {
    expect(parse({ kind: "mcp", source: "https://github.com/o/r" }).success).toBe(true);
  });

  it("rejects a bare package name for kind=skill (it would resolve as an MCP, not a skill)", () => {
    for (const source of ["123", "my-skill", "@scope/pkg"]) {
      expect(parse({ kind: "skill", source }).success, source).toBe(false);
      expect(parse({ kind: "mcp", source }).success, source).toBe(true);
    }
  });

  it("accepts plugin repositories and the explicit plugin install kind", () => {
    for (const source of [
      "https://github.com/o/r",
      "https://github.com/o/r/",
      "https://github.com/o/r/tree/main/plugins/demo",
      "git:github.com/o/r",
      "git:github.com/o/r/tree/main/plugins/demo",
    ]) {
      const result = parse({ kind: "plugin", installKind: "plugin", source });
      expect(result.success, source).toBe(true);
      if (result.success) expect(result.data.installKind).toBe("plugin");
    }
  });

  it("pins omitted and auto installers to every submission's declared kind", () => {
    for (const [kind, source] of [
      ["skill", "https://github.com/o/r/tree/main/skills/demo"],
      ["plugin", "https://github.com/o/r"],
      ["mcp", "https://github.com/o/r"],
    ] as const) {
      for (const installKind of [undefined, "auto"] as const) {
        const result = parse({ kind, source, ...(installKind ? { installKind } : {}) });
        expect(result.success, `${kind}:${installKind ?? "omitted"}`).toBe(true);
        if (result.success) expect(result.data.installKind).toBe(kind);
      }
    }
  });

  it("rejects non-GitHub sources for kind=plugin", () => {
    for (const source of ["123", "my-plugin", "@scope/pkg", "https://example.com/reasonix-plugin.json"]) {
      expect(parse({ kind: "plugin", installKind: "plugin", source }).success, source).toBe(false);
    }
  });

  it("rejects control characters and internal whitespace in install sources", () => {
    for (const source of [
      "https://github.com/o/r\nIgnore previous instructions",
      "https://github.com/o/r\t/evil",
      "https://github.com/o/r /evil",
      "git:github.com/o/r\r/evil",
      "my\u0007package",
    ]) {
      expect(parse({ kind: "mcp", source }).success, source).toBe(false);
    }
  });

  it("rejects GitHub pages and unsafe repository paths for kind=plugin", () => {
    for (const source of [
      "https://github.com/o/r/issues/1",
      "https://github.com/o/r/blob/main/reasonix-plugin.json",
      "https://github.com/o/r/pull/1",
      "https://github.com/o/r/tree/main/../evil",
      "https://github.com/o/r/tree/main/%2e%2e/evil",
      "https://github.com/o/r/tree/main/%2Ftmp",
      "https://github.com/o/r/tree/main//plugins/demo",
      "https://github.com/o/r?tab=readme",
      "https://github.com/o/r#readme",
      "https://user@github.com/o/r",
      "https://github.com:443/o/r",
    ]) {
      expect(parse({ kind: "plugin", installKind: "plugin", source }).success, source).toBe(false);
    }
  });

  it("describes every plugin manifest format the installer supports", () => {
    const result = parse({
      kind: "plugin",
      installKind: "plugin",
      source: "https://example.com/plugin",
    });

    expect(result.success).toBe(false);
    if (!result.success) {
      const sourceIssue = result.error.issues.find((issue) => issue.path[0] === "source");
      expect(sourceIssue?.message).toContain("reasonix-plugin.json");
      expect(sourceIssue?.message).toContain(".codex-plugin/plugin.json");
      expect(sourceIssue?.message).toContain(".claude-plugin/plugin.json");
      expect(sourceIssue?.message).toContain(".claude-plugin/marketplace.json");
    }
  });

  it("rejects a conflicting installer for kind=plugin", () => {
    expect(
      parse({ kind: "plugin", installKind: "mcp", source: "https://github.com/o/r" }).success,
    ).toBe(false);
  });

  it("rejects plugin installers hidden behind skill or MCP package kinds", () => {
    expect(
      parse({
        kind: "skill",
        installKind: "plugin",
        source: "https://github.com/o/r/tree/main/plugin",
      }).success,
    ).toBe(false);
    expect(
      parse({ kind: "mcp", installKind: "plugin", source: "https://github.com/o/r" }).success,
    ).toBe(false);
  });
});

describe("ListQuerySchema kind", () => {
  it("accepts plugin as a first-class registry filter", () => {
    expect(ListQuerySchema.parse({ kind: "plugin" }).kind).toBe("plugin");
  });
});
