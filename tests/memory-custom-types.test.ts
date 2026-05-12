/** User-defined memory types — config-driven priority + expires (#709). */

import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  type CustomMemoryTypeConfig,
  type ReasonixConfig,
  loadMemoryTypeRegistry,
  memoryTypeDefaults,
} from "../src/config.js";
import { MemoryStore, applyUserMemory, effectivePriority } from "../src/memory/user.js";

function cfgWith(types: CustomMemoryTypeConfig[]): ReasonixConfig {
  return { memory: { customTypes: types } };
}

describe("custom memory types (#709)", () => {
  let home: string;
  let projectRoot: string;
  const originalEnv = process.env.REASONIX_MEMORY;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-memtype-home-"));
    projectRoot = mkdtempSync(join(tmpdir(), "reasonix-memtype-proj-"));
    // biome-ignore lint/performance/noDelete: avoid leaking "undefined" into env
    delete process.env.REASONIX_MEMORY;
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
    rmSync(projectRoot, { recursive: true, force: true });
    if (originalEnv === undefined) {
      // biome-ignore lint/performance/noDelete: same
      delete process.env.REASONIX_MEMORY;
    } else {
      process.env.REASONIX_MEMORY = originalEnv;
    }
  });

  describe("loadMemoryTypeRegistry", () => {
    it("returns the four built-ins when no config is provided", () => {
      const reg = loadMemoryTypeRegistry({});
      expect(reg.map((r) => r.name)).toEqual(["user", "feedback", "project", "reference"]);
      expect(reg.every((r) => r.builtin)).toBe(true);
    });

    it("overlays validated custom types with defaults", () => {
      const reg = loadMemoryTypeRegistry(
        cfgWith([
          { name: "security", priority: "high" },
          { name: "design_system", priority: "medium" },
          { name: "deploy_checklist", priority: "medium", expires: "project_end" },
        ]),
      );
      const security = reg.find((r) => r.name === "security");
      expect(security?.builtin).toBe(false);
      expect(security?.priority).toBe("high");
      expect(reg.find((r) => r.name === "deploy_checklist")?.expires).toBe("project_end");
    });

    it("rejects invalid type names and conflicting built-ins", () => {
      const reg = loadMemoryTypeRegistry(
        cfgWith([
          { name: "../etc" } as CustomMemoryTypeConfig,
          { name: "" } as CustomMemoryTypeConfig,
          { name: "user", priority: "high" },
        ]),
      );
      expect(reg.filter((r) => r.builtin).length).toBe(4);
      expect(reg.find((r) => r.name === "user")?.builtin).toBe(true);
      expect(reg.find((r) => r.name === "../etc")).toBeUndefined();
    });
  });

  describe("MemoryStore round-trip with priority + expires", () => {
    it("persists and reads priority + expires fields", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "no-secrets",
        type: "security",
        scope: "global",
        description: "never hardcode credentials in source files",
        body: "API keys live in env files only. Reject any diff that inlines a secret.",
        priority: "high",
      });
      const file = store.pathFor("global", "no-secrets");
      const raw = readFileSync(file, "utf8");
      expect(raw).toContain("type: security");
      expect(raw).toContain("priority: high");

      const back = store.read("global", "no-secrets");
      expect(back.type).toBe("security");
      expect(back.priority).toBe("high");
      expect(back.expires).toBeUndefined();
    });

    it("round-trips `expires: project_end`", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "release-freeze",
        type: "deploy_checklist",
        scope: "global",
        description: "no merges until release branch cuts",
        body: "Hold non-critical merges until 2026-05-15.",
        expires: "project_end",
      });
      const back = store.read("global", "release-freeze");
      expect(back.expires).toBe("project_end");
    });
  });

  describe("effectivePriority", () => {
    it("uses the entry's own priority when present", () => {
      expect(
        effectivePriority({
          name: "x",
          type: "reference",
          scope: "global",
          description: "",
          body: "",
          createdAt: "",
          priority: "high",
        }),
      ).toBe("high");
    });

    it("falls back to config default for the type", () => {
      const cfg = cfgWith([{ name: "security", priority: "high" }]);
      expect(
        effectivePriority(
          {
            name: "x",
            type: "security",
            scope: "global",
            description: "",
            body: "",
            createdAt: "",
          },
          cfg,
        ),
      ).toBe("high");
    });

    it("returns undefined when neither the entry nor the type registers a priority", () => {
      expect(
        effectivePriority({
          name: "x",
          type: "reference",
          scope: "global",
          description: "",
          body: "",
          createdAt: "",
        }),
      ).toBeUndefined();
    });
  });

  describe("memoryTypeDefaults", () => {
    it("returns config-declared priority + expires for a known custom type", () => {
      const cfg = cfgWith([
        { name: "deploy_checklist", priority: "medium", expires: "project_end" },
      ]);
      expect(memoryTypeDefaults("deploy_checklist", cfg)).toEqual({
        priority: "medium",
        expires: "project_end",
      });
    });

    it("returns empty defaults for built-ins (they have no priority by default)", () => {
      expect(memoryTypeDefaults("project", {})).toEqual({});
    });

    it("returns empty defaults for entirely unknown types", () => {
      expect(memoryTypeDefaults("something_unregistered", {})).toEqual({});
    });
  });

  describe("applyUserMemory injects HIGH PRIORITY block", () => {
    it("prepends a HIGH PRIORITY section when any entry resolves to priority: high", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "no-prod-writes",
        type: "security",
        scope: "global",
        description: "never run write queries against prod DBs",
        body: "All migrations go through the staging pipeline first.",
        priority: "high",
      });
      store.write({
        name: "naming-style",
        type: "user",
        scope: "global",
        description: "use kebab-case for filenames",
        body: "kebab-case throughout.",
      });

      const out = applyUserMemory("BASE", { homeDir: home, projectRoot });
      expect(out).toContain("# HIGH PRIORITY constraints (must observe)");
      expect(out).toContain("!!! [global/security/no-prod-writes]");
      expect(out).toContain("never run write queries against prod DBs");
      expect(out).toContain("# User memory — global");
      // Regular naming-style entry is in the regular index, not the high-priority block:
      const high = out.split("# User memory")[0] ?? "";
      expect(high).not.toContain("naming-style");
    });

    it("does not produce a HIGH PRIORITY block when no entry is high-priority", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "naming-style",
        type: "user",
        scope: "global",
        description: "use kebab-case for filenames",
        body: "kebab-case throughout.",
      });
      const out = applyUserMemory("BASE", { homeDir: home, projectRoot });
      expect(out).not.toContain("HIGH PRIORITY constraints");
    });

    it("treats config-driven priority for a custom type as high priority", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "modal-component",
        type: "design_system",
        scope: "global",
        description: "all modals must use the shared Modal component",
        body: "Use @ui/Modal — never roll your own.",
      });

      const cfg = cfgWith([{ name: "design_system", priority: "high" }]);
      const out = applyUserMemory("BASE", { homeDir: home, projectRoot, cfg });
      expect(out).toContain("# HIGH PRIORITY constraints (must observe)");
      expect(out).toContain("!!! [global/design_system/modal-component]");
    });
  });

  describe("unknown types fall through verbatim", () => {
    it("accepts any string for `type` and preserves it on read", () => {
      const store = new MemoryStore({ homeDir: home, projectRoot });
      store.write({
        name: "experimental",
        type: "performance_budget",
        scope: "global",
        description: "p99 latency budget",
        body: "p99 < 200ms.",
      });
      const back = store.read("global", "experimental");
      expect(back.type).toBe("performance_budget");
    });
  });
});

it("exists", () => {
  // top-level placeholder so `vitest --reporter=verbose` shows the file even if all describe blocks are skipped
  expect(existsSync(__filename)).toBe(true);
});
