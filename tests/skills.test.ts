/** Skills store + prefix-index composer — temp homeDir / projectRoot per test, no real skill dirs touched. */

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { SkillStore, applySkillsIndex, validateSkillFrontmatter } from "../src/skills.js";

const BASE = "You are a test assistant.";

type SkillRoot = "project" | "global" | "custom";

function writeSkillDir(
  root: string,
  which: SkillRoot,
  name: string,
  frontmatter: Record<string, string>,
  body: string,
  homeOrProject: string,
): string {
  const parent =
    which === "global"
      ? join(homeOrProject, ".reasonix", "skills")
      : which === "project"
        ? join(root, ".reasonix", "skills")
        : homeOrProject;
  const dir = join(parent, name);
  mkdirSync(dir, { recursive: true });
  const fmLines = ["---"];
  for (const [k, v] of Object.entries(frontmatter)) fmLines.push(`${k}: ${v}`);
  fmLines.push("---", "");
  const path = join(dir, "SKILL.md");
  writeFileSync(path, `${fmLines.join("\n")}${body}\n`, "utf8");
  return path;
}

function writeFlatSkill(
  dir: string,
  name: string,
  frontmatter: Record<string, string>,
  body: string,
): string {
  const skills = join(dir, ".reasonix", "skills");
  mkdirSync(skills, { recursive: true });
  const fmLines = ["---"];
  for (const [k, v] of Object.entries(frontmatter)) fmLines.push(`${k}: ${v}`);
  fmLines.push("---", "");
  const path = join(skills, `${name}.md`);
  writeFileSync(path, `${fmLines.join("\n")}${body}\n`, "utf8");
  return path;
}

describe("SkillStore", () => {
  let home: string;
  let projectRoot: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-skills-home-"));
    projectRoot = mkdtempSync(join(tmpdir(), "reasonix-skills-proj-"));
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
    rmSync(projectRoot, { recursive: true, force: true });
  });

  it("returns an empty list when no skill dirs exist", () => {
    const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
    expect(store.list()).toEqual([]);
  });

  it("hasProjectScope reflects constructor argument", () => {
    expect(new SkillStore({ homeDir: home, disableBuiltins: true }).hasProjectScope()).toBe(false);
    expect(
      new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true }).hasProjectScope(),
    ).toBe(true);
  });

  it("parses a SKILL.md dir-layout entry from the global scope", () => {
    writeSkillDir(
      projectRoot,
      "global",
      "review",
      { name: "review", description: "Review a pull request" },
      "Run `git diff` and summarize risks.",
      home,
    );
    const skills = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true }).list();
    expect(skills).toHaveLength(1);
    expect(skills[0]?.name).toBe("review");
    expect(skills[0]?.scope).toBe("global");
    expect(skills[0]?.body).toContain("git diff");
  });

  it("reads flat <name>.md files as well", () => {
    writeFlatSkill(home, "ship-it", { description: "Commit and push changes" }, "body");
    const skills = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true }).list();
    expect(skills.map((s) => s.name)).toEqual(["ship-it"]);
    expect(skills[0]?.description).toBe("Commit and push changes");
  });

  it("surfaces project-scope skills from <projectRoot>/.reasonix/skills", () => {
    writeSkillDir(
      projectRoot,
      "project",
      "deploy",
      { description: "Deploy to staging" },
      "Run the staging pipeline.",
      home,
    );
    const list = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true }).list();
    expect(list).toHaveLength(1);
    expect(list[0]?.scope).toBe("project");
    expect(list[0]?.path).toContain(projectRoot);
  });

  it("project scope wins on a name collision with global", () => {
    writeSkillDir(projectRoot, "global", "review", { description: "global one" }, "G", home);
    writeSkillDir(projectRoot, "project", "review", { description: "project one" }, "P", home);
    const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
    const list = store.list();
    expect(list).toHaveLength(1);
    expect(list[0]?.scope).toBe("project");
    expect(list[0]?.description).toBe("project one");
    expect(store.read("review")?.body).toBe("P");
  });

  it("without projectRoot the store only reads the global scope", () => {
    // Put a skill in the project dir and a skill in the global dir.
    writeSkillDir(projectRoot, "project", "deploy", { description: "proj" }, "P", home);
    writeSkillDir(projectRoot, "global", "review", { description: "glob" }, "G", home);
    const store = new SkillStore({ homeDir: home, disableBuiltins: true }); // no projectRoot
    const names = store.list().map((s) => s.name);
    expect(names).toEqual(["review"]);
    expect(store.hasProjectScope()).toBe(false);
  });

  it("rejects invalid skill names on read()", () => {
    const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
    expect(store.read("../etc/passwd")).toBeNull();
    expect(store.read("foo/bar")).toBeNull();
    expect(store.read("")).toBeNull();
  });

  it("skips dotfiles that would masquerade as skills", () => {
    writeSkillDir(projectRoot, "global", "ok", { description: "fine" }, "body", home);
    const dotDir = join(home, ".reasonix", "skills");
    writeFileSync(join(dotDir, ".hidden.md"), "---\ndescription: x\n---\nbody\n", "utf8");
    const list = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true }).list();
    expect(list.map((s) => s.name)).toEqual(["ok"]);
  });

  it("reads custom flat and dir-layout skills directly from configured roots", () => {
    const custom = mkdtempSync(join(tmpdir(), "reasonix-skills-custom-"));
    try {
      writeSkillDir(
        projectRoot,
        "custom",
        "custom-dir",
        { description: "custom dir" },
        "D",
        custom,
      );
      const flatPath = join(custom, "custom-flat.md");
      writeFileSync(flatPath, "---\ndescription: custom flat\n---\nF\n", "utf8");
      const list = new SkillStore({
        homeDir: home,
        projectRoot,
        customSkillPaths: [custom],
        disableBuiltins: true,
      }).list();
      expect(list.map((s) => `${s.scope}:${s.name}`)).toEqual([
        "custom:custom-dir",
        "custom:custom-flat",
      ]);
      expect(list.find((s) => s.name === "custom-flat")?.path).toBe(flatPath);
    } finally {
      rmSync(custom, { recursive: true, force: true });
    }
  });

  it("deduplicates custom roots and preserves first priority", () => {
    const custom = mkdtempSync(join(tmpdir(), "reasonix-skills-custom-"));
    try {
      const roots = new SkillStore({
        homeDir: home,
        projectRoot,
        customSkillPaths: [custom, custom],
        disableBuiltins: true,
      }).customRoots();
      expect(roots.map((r) => r.dir)).toEqual([custom]);
    } finally {
      rmSync(custom, { recursive: true, force: true });
    }
  });

  it("resolves relative custom roots against projectRoot for discovery", () => {
    const relativeRoot = "skills-local";
    const custom = join(projectRoot, relativeRoot);
    writeSkillDir(projectRoot, "custom", "local-skill", { description: "local" }, "L", custom);
    const store = new SkillStore({
      homeDir: home,
      projectRoot,
      customSkillPaths: [relativeRoot, custom],
      disableBuiltins: true,
    });
    expect(store.customRoots().map((r) => r.dir)).toEqual([resolve(projectRoot, relativeRoot)]);
    expect(store.list().map((s) => s.name)).toEqual(["local-skill"]);
  });

  it("keeps priority project > custom > global on same-name collisions", () => {
    const custom = mkdtempSync(join(tmpdir(), "reasonix-skills-custom-"));
    try {
      writeSkillDir(projectRoot, "global", "same", { description: "global" }, "G", home);
      writeSkillDir(projectRoot, "custom", "same", { description: "custom" }, "C", custom);
      const customWinner = new SkillStore({
        homeDir: home,
        projectRoot,
        customSkillPaths: [custom],
        disableBuiltins: true,
      }).read("same");
      expect(customWinner?.scope).toBe("custom");
      writeSkillDir(projectRoot, "project", "same", { description: "project" }, "P", home);
      const projectWinner = new SkillStore({
        homeDir: home,
        projectRoot,
        customSkillPaths: [custom],
        disableBuiltins: true,
      }).read("same");
      expect(projectWinner?.scope).toBe("project");
    } finally {
      rmSync(custom, { recursive: true, force: true });
    }
  });

  it("reports invalid custom root status without throwing", () => {
    const missing = join(projectRoot, "missing-skills");
    const notDir = join(projectRoot, "file.txt");
    writeFileSync(notDir, "x", "utf8");
    const store = new SkillStore({
      homeDir: home,
      projectRoot,
      customSkillPaths: [missing, notDir],
      disableBuiltins: true,
    });
    expect(store.list()).toEqual([]);
    expect(store.customRoots().map((r) => r.status)).toEqual(["missing", "not-directory"]);
  });

  describe("create() — /skill new scaffold (#366)", () => {
    it("writes a project-scope stub when projectRoot is set", () => {
      const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
      const r = store.create("frontend-writer", "project");
      expect("path" in r).toBe(true);
      const list = store.list();
      const made = list.find((s) => s.name === "frontend-writer");
      expect(made?.scope).toBe("project");
      expect(made?.description).toMatch(/one-liner/i);
    });

    it("falls back to global scope when projectRoot is absent", () => {
      const store = new SkillStore({ homeDir: home, disableBuiltins: true });
      const r = store.create("global-skill", "global");
      expect("path" in r).toBe(true);
      const list = store.list();
      expect(list.find((s) => s.name === "global-skill")?.scope).toBe("global");
    });

    it("refuses to overwrite an existing skill", () => {
      const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
      store.create("dup", "project");
      const second = store.create("dup", "project");
      expect("error" in second).toBe(true);
    });

    it("rejects invalid skill names", () => {
      const store = new SkillStore({ homeDir: home, projectRoot, disableBuiltins: true });
      const r = store.create("../etc/passwd", "project");
      expect("error" in r).toBe(true);
    });

    it("refuses project scope when no projectRoot is configured", () => {
      const store = new SkillStore({ homeDir: home, disableBuiltins: true });
      const r = store.create("nope", "project");
      expect("error" in r).toBe(true);
    });
  });
});

describe("applySkillsIndex", () => {
  let home: string;
  let projectRoot: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-skills-idx-"));
    projectRoot = mkdtempSync(join(tmpdir(), "reasonix-skills-idx-proj-"));
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
    rmSync(projectRoot, { recursive: true, force: true });
  });

  it("returns the prompt unchanged when no skills exist", () => {
    const out = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    expect(out).toBe(BASE);
  });

  it("emits a Skills section with one-liners but not bodies", () => {
    writeSkillDir(
      projectRoot,
      "global",
      "review",
      { description: "Review a pull request" },
      "BODY-THAT-MUST-NOT-APPEAR",
      home,
    );
    writeSkillDir(
      projectRoot,
      "global",
      "init",
      { description: "Initialize a CLAUDE.md" },
      "ALSO-SECRET",
      home,
    );
    const out = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    expect(out).toContain("# Skills");
    expect(out).toContain("- init — Initialize a CLAUDE.md");
    expect(out).toContain("- review — Review a pull request");
    expect(out).not.toContain("BODY-THAT-MUST-NOT-APPEAR");
    expect(out).not.toContain("ALSO-SECRET");
  });

  it("merges project + global skills into a single index", () => {
    writeSkillDir(projectRoot, "global", "hello", { description: "global hello" }, "x", home);
    writeSkillDir(projectRoot, "project", "deploy", { description: "project deploy" }, "y", home);
    const out = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    expect(out).toContain("- deploy — project deploy");
    expect(out).toContain("- hello — global hello");
  });

  it("surfaces skills with blank descriptions using a placeholder so the model can name + flag them (#583)", () => {
    writeSkillDir(projectRoot, "global", "has-desc", { description: "I have one" }, "body", home);
    writeSkillDir(projectRoot, "global", "no-desc", {}, "body", home);
    const out = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    expect(out).toContain("- has-desc — I have one");
    expect(out).toContain("- no-desc");
    expect(out).toContain('"description:"');
  });

  it("is byte-stable across two calls with the same filesystem state", () => {
    writeSkillDir(projectRoot, "global", "a", { description: "one" }, "x", home);
    writeSkillDir(projectRoot, "global", "b", { description: "two" }, "y", home);
    const first = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    const second = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    expect(first).toBe(second);
  });

  it("tags subagent-runAs skills AFTER the name in the index (not before)", () => {
    writeSkillDir(
      projectRoot,
      "global",
      "lookup",
      { description: "Look something up", runAs: "subagent" },
      "body",
      home,
    );
    writeSkillDir(
      projectRoot,
      "global",
      "fmt",
      { description: "Format the codebase", runAs: "inline" },
      "body",
      home,
    );
    const out = applySkillsIndex(BASE, { homeDir: home, projectRoot, disableBuiltins: true });
    // Name-first, tag-after: prevents the model from copying "🧬 lookup"
    // as the skill name into `run_skill({ name: ... })`.
    expect(out).toContain("- lookup [🧬 subagent] — Look something up");
    expect(out).toContain("- fmt — Format the codebase");
    // Old "🧬 name" format must not regress — there was a user bug where
    // the model copied the marker verbatim and run_skill failed lookup.
    expect(out).not.toMatch(/- 🧬 lookup\b/);
    expect(out).not.toContain("- 🧬 fmt");
  });
});

describe("validateSkillFrontmatter (#583 install gate)", () => {
  it("accepts content with a non-empty description line", () => {
    const result = validateSkillFrontmatter("---\ndescription: does a thing\n---\nbody\n");
    expect(result).toEqual({ ok: true });
  });

  it("rejects content with no frontmatter at all", () => {
    const result = validateSkillFrontmatter("# just a body\n");
    expect("error" in result && result.error).toMatch(/description/);
  });

  it("rejects frontmatter that omits description", () => {
    const result = validateSkillFrontmatter("---\nname: foo\n---\nbody\n");
    expect("error" in result && result.error).toMatch(/description/);
  });

  it("rejects frontmatter where description is whitespace-only", () => {
    const result = validateSkillFrontmatter("---\ndescription:    \n---\nbody\n");
    expect("error" in result && result.error).toMatch(/description/);
  });
});

describe("Skill frontmatter — runAs", () => {
  let home: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-skills-runas-"));
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
  });

  it("defaults runAs to inline when frontmatter omits it", () => {
    writeSkillDir(home, "global", "plain", { description: "plain skill" }, "body", home);
    const skill = new SkillStore({ homeDir: home, disableBuiltins: true }).read("plain");
    expect(skill?.runAs).toBe("inline");
  });

  it("parses runAs: subagent from frontmatter", () => {
    writeSkillDir(
      home,
      "global",
      "deep",
      { description: "deep dive", runAs: "subagent" },
      "body",
      home,
    );
    const skill = new SkillStore({ homeDir: home, disableBuiltins: true }).read("deep");
    expect(skill?.runAs).toBe("subagent");
  });

  it("falls back to inline for any unknown runAs value", () => {
    writeSkillDir(home, "global", "weird", { description: "?", runAs: "parallel" }, "body", home);
    const skill = new SkillStore({ homeDir: home, disableBuiltins: true }).read("weird");
    expect(skill?.runAs).toBe("inline");
  });

  it("captures a deepseek-* model override and ignores anything else", () => {
    writeSkillDir(
      home,
      "global",
      "rsr",
      { description: "...", runAs: "subagent", model: "deepseek-reasoner" },
      "body",
      home,
    );
    writeSkillDir(
      home,
      "global",
      "wrong",
      { description: "...", runAs: "subagent", model: "gpt-4" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("rsr")?.model).toBe("deepseek-reasoner");
    expect(store.read("wrong")?.model).toBeUndefined();
  });

  it("parses comma-separated allowed-tools into a trimmed list", () => {
    writeSkillDir(
      home,
      "global",
      "scoped",
      { description: "...", runAs: "subagent", "allowed-tools": "read, search_content,write" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("scoped")?.allowedTools).toEqual(["read", "search_content", "write"]);
  });

  it("treats missing allowed-tools as undefined (full inheritance)", () => {
    writeSkillDir(home, "global", "open", { description: "...", runAs: "subagent" }, "body", home);
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("open")?.allowedTools).toBeUndefined();
  });

  it("treats an allowed-tools field with only whitespace/commas as undefined", () => {
    writeSkillDir(
      home,
      "global",
      "empty",
      { description: "...", runAs: "subagent", "allowed-tools": " , , " },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("empty")?.allowedTools).toBeUndefined();
  });

  it("parses max-iters and passes it through as maxToolIters", () => {
    writeSkillDir(
      home,
      "global",
      "big",
      { description: "...", runAs: "subagent", "max-iters": "32" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("big")?.maxToolIters).toBe(32);
  });

  it("passes large max-iters values through unchanged (no upper clamp)", () => {
    writeSkillDir(
      home,
      "global",
      "bigger",
      { description: "...", runAs: "subagent", "max-iters": "9999" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("bigger")?.maxToolIters).toBe(9999);
  });

  it("ignores max-iters below 1 (falls back to subagent default)", () => {
    writeSkillDir(
      home,
      "global",
      "toosmall",
      { description: "...", runAs: "subagent", "max-iters": "0" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("toosmall")?.maxToolIters).toBeUndefined();
  });

  it("ignores max-iters that isn't a number", () => {
    writeSkillDir(
      home,
      "global",
      "junk",
      { description: "...", runAs: "subagent", "max-iters": "many" },
      "body",
      home,
    );
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("junk")?.maxToolIters).toBeUndefined();
  });
});

describe("Built-in skills", () => {
  let home: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-skills-builtins-"));
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
  });

  it("ships explore/research/review/security-review/test as builtins", () => {
    const store = new SkillStore({ homeDir: home }); // builtins ON
    const names = store.list().map((s) => s.name);
    expect(names).toContain("explore");
    expect(names).toContain("research");
    expect(names).toContain("review");
    expect(names).toContain("security-review");
    expect(names).toContain("test");
    const explore = store.read("explore");
    expect(explore?.runAs).toBe("subagent");
    expect(explore?.scope).toBe("builtin");
    const research = store.read("research");
    expect(research?.runAs).toBe("subagent");
    const review = store.read("review");
    expect(review?.runAs).toBe("subagent");
    expect(review?.scope).toBe("builtin");
    // Review's body must mention the read-only contract — that's the
    // load-bearing rule that distinguishes review from "do the change."
    expect(review?.body).toMatch(/read-only/i);
    const sec = store.read("security-review");
    expect(sec?.runAs).toBe("subagent");
    expect(sec?.body).toMatch(/injection/i);
    expect(sec?.body).toMatch(/CRITICAL|critical/);
    // /test is INLINE on purpose — parent must see the proposed edits.
    const test = store.read("test");
    expect(test?.runAs).toBe("inline");
    expect(test?.body).toMatch(/run_command/);
    expect(test?.body).toMatch(/SEARCH\/REPLACE/);
  });

  it("user-authored skills override a builtin with the same name", () => {
    writeSkillDir(home, "global", "explore", { description: "my own" }, "custom body", home);
    const store = new SkillStore({ homeDir: home });
    const explore = store.read("explore");
    expect(explore?.scope).toBe("global");
    expect(explore?.body).toBe("custom body");
  });

  it("disableBuiltins hides them entirely", () => {
    const store = new SkillStore({ homeDir: home, disableBuiltins: true });
    expect(store.read("explore")).toBeNull();
    expect(store.list()).toEqual([]);
  });

  it("builtins surface with the subagent tag after the name in applySkillsIndex", () => {
    const out = applySkillsIndex(BASE, { homeDir: home }); // builtins ON
    expect(out).toContain("# Skills");
    expect(out).toContain("explore [🧬 subagent]");
    expect(out).toContain("research [🧬 subagent]");
    expect(out).toContain("review [🧬 subagent]");
    expect(out).toContain("security-review [🧬 subagent]");
    // /test is inline → no subagent tag
    expect(out).toContain("test —");
    expect(out).not.toContain("test [🧬 subagent]");
  });
});
