import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { handleSlash } from "../src/cli/ui/slash/dispatch.js";
import {
  addProjectShellAllowed,
  loadGlobalShellAllowed,
  loadProjectShellAllowed,
} from "../src/config.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../src/index.js";
import { ToolRegistry } from "../src/tools.js";

function makeLoop(): CacheFirstLoop {
  return new CacheFirstLoop({
    client: new DeepSeekClient({ apiKey: "sk-test" }),
    prefix: new ImmutablePrefix({ system: "s", toolSpecs: [] }),
    tools: new ToolRegistry(),
    maxToolIters: 1,
    stream: false,
  });
}

describe("/permissions slash handler", () => {
  let dir: string;
  let cfgPath: string;
  let projectRoot: string;
  const originalHome = process.env.HOME;
  const originalUserProfile = process.env.USERPROFILE;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "reasonix-perms-slash-"));
    cfgPath = join(dir, "config.json");
    projectRoot = join(dir, "project");
    // Redirect ~/.reasonix → temp dir so the handler's calls (which use
    // defaultConfigPath) land in `cfgPath`. config.test.ts skips this by
    // passing `path` explicitly to every helper, but the slash handler
    // hardcodes the default — so we have to redirect HOME instead.
    process.env.HOME = dir;
    process.env.USERPROFILE = dir;
  });

  afterEach(() => {
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true });
    if (originalHome === undefined) {
      // biome-ignore lint/performance/noDelete: the string "undefined" leaks into process.env otherwise
      delete process.env.HOME;
    } else {
      process.env.HOME = originalHome;
    }
    if (originalUserProfile === undefined) {
      // biome-ignore lint/performance/noDelete: same reason
      delete process.env.USERPROFILE;
    } else {
      process.env.USERPROFILE = originalUserProfile;
    }
  });

  it("bare /permissions lists builtin entries (always shown)", () => {
    const result = handleSlash("permissions", [], makeLoop(), {});
    expect(result.info).toMatch(/Builtin allowlist/);
    expect(result.info).toMatch(/git/);
    expect(result.info).toMatch(/Project allowlist/);
  });

  it("bare /permissions surfaces yolo-mode banner when active", () => {
    const result = handleSlash("permissions", [], makeLoop(), {
      codeRoot: projectRoot,
      editMode: "yolo",
    });
    expect(result.info).toMatch(/YOLO/);
    expect(result.info).toMatch(/bypassed/);
  });

  it("bare /permissions lists project entries with 1-based indices", () => {
    addProjectShellAllowed(projectRoot, "npm run build", join(dir, ".reasonix", "config.json"));
    addProjectShellAllowed(projectRoot, "deploy.sh", join(dir, ".reasonix", "config.json"));
    const result = handleSlash("permissions", [], makeLoop(), {
      codeRoot: projectRoot,
      editMode: "review",
    });
    expect(result.info).toMatch(/1\.\s+npm run build/);
    expect(result.info).toMatch(/2\.\s+deploy\.sh/);
  });

  it("/permissions add persists a new prefix", () => {
    const result = handleSlash("permissions", ["add", "npm", "run", "build"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/added.*npm run build/);
    expect(loadProjectShellAllowed(projectRoot, join(dir, ".reasonix", "config.json"))).toContain(
      "npm run build",
    );
  });

  it("/permissions add rejects an empty prefix with a usage hint", () => {
    const result = handleSlash("permissions", ["add"], makeLoop(), { codeRoot: projectRoot });
    expect(result.info).toMatch(/usage:\s+\/permissions add/);
  });

  it("/permissions add notes when the target is already in the builtin list", () => {
    const result = handleSlash("permissions", ["add", "git", "status"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/builtin allowlist/i);
    // Should NOT have written a redundant project entry.
    expect(loadProjectShellAllowed(projectRoot, join(dir, ".reasonix", "config.json"))).toEqual([]);
  });

  it("/permissions add --global routes to the global list (project untouched, #2059)", () => {
    const cfg = join(dir, ".reasonix", "config.json");
    const result = handleSlash("permissions", ["add", "--global", "brew", "install"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/global allowlist/i);
    expect(loadGlobalShellAllowed(cfg)).toContain("brew install");
    expect(loadProjectShellAllowed(projectRoot, cfg)).toEqual([]);
  });

  it("/permissions add `g` is a project prefix, not hijacked as the global keyword (#2059)", () => {
    const cfg = join(dir, ".reasonix", "config.json");
    const result = handleSlash("permissions", ["add", "g"], makeLoop(), { codeRoot: projectRoot });
    expect(result.info).toMatch(/added.*g/);
    expect(loadProjectShellAllowed(projectRoot, cfg)).toContain("g");
    expect(loadGlobalShellAllowed(cfg)).toEqual([]);
  });

  it("/permissions add --global works without a project root (chat mode)", () => {
    const cfg = join(dir, ".reasonix", "config.json");
    const result = handleSlash("permissions", ["add", "--global", "make", "test"], makeLoop(), {});
    // Should NOT bail out with the "mutateCodeOnly" hint — global needs no root.
    expect(result.info).not.toMatch(/reasonix code/i);
    expect(loadGlobalShellAllowed(cfg)).toContain("make test");
  });

  it("/permissions add preserves an inner --global inside a real command prefix (#2059)", () => {
    // Without leading-only flag parsing, `--global` mid-prefix would be stripped,
    // landing the wrong prefix in the GLOBAL list. After the fix it must add the
    // full prefix to PROJECT.
    const cfg = join(dir, ".reasonix", "config.json");
    const result = handleSlash(
      "permissions",
      ["add", "git", "config", "--global", "user.name"],
      makeLoop(),
      { codeRoot: projectRoot },
    );
    expect(result.info).toMatch(/added/);
    expect(loadProjectShellAllowed(projectRoot, cfg)).toContain("git config --global user.name");
    expect(loadGlobalShellAllowed(cfg)).toEqual([]);
  });

  it("/permissions add --global still applies when followed by a prefix containing --global", () => {
    // Leading flag → global; inner flag → preserved verbatim.
    const cfg = join(dir, ".reasonix", "config.json");
    const result = handleSlash(
      "permissions",
      ["add", "--global", "npm", "install", "--global", "foo"],
      makeLoop(),
      { codeRoot: projectRoot },
    );
    expect(result.info).toMatch(/global allowlist/i);
    expect(loadGlobalShellAllowed(cfg)).toContain("npm install --global foo");
    expect(loadProjectShellAllowed(projectRoot, cfg)).toEqual([]);
  });

  it("/permissions remove drops by exact prefix", () => {
    const cfgFile = join(dir, ".reasonix", "config.json");
    addProjectShellAllowed(projectRoot, "npm run build", cfgFile);
    addProjectShellAllowed(projectRoot, "deploy.sh", cfgFile);
    const result = handleSlash("permissions", ["remove", "deploy.sh"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/removed.*deploy\.sh/);
    expect(loadProjectShellAllowed(projectRoot, cfgFile)).toEqual(["npm run build"]);
  });

  it("/permissions remove drops by 1-based project index", () => {
    const cfgFile = join(dir, ".reasonix", "config.json");
    addProjectShellAllowed(projectRoot, "alpha", cfgFile);
    addProjectShellAllowed(projectRoot, "beta", cfgFile);
    addProjectShellAllowed(projectRoot, "gamma", cfgFile);
    const result = handleSlash("permissions", ["remove", "2"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/removed.*beta/);
    expect(loadProjectShellAllowed(projectRoot, cfgFile)).toEqual(["alpha", "gamma"]);
  });

  it("/permissions remove flags an out-of-range index", () => {
    const cfgFile = join(dir, ".reasonix", "config.json");
    addProjectShellAllowed(projectRoot, "alpha", cfgFile);
    const result = handleSlash("permissions", ["remove", "5"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/index out of range/);
  });

  it("/permissions remove of an unknown prefix replies 'no such project entry'", () => {
    const result = handleSlash("permissions", ["remove", "ghost"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/no such project entry/);
  });

  it("/permissions remove on a builtin entry refuses (read-only)", () => {
    const result = handleSlash("permissions", ["remove", "git", "status"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/builtin/i);
    expect(result.info).toMatch(/can't be removed/i);
  });

  it("/permissions clear without 'confirm' asks for confirmation", () => {
    const cfgFile = join(dir, ".reasonix", "config.json");
    addProjectShellAllowed(projectRoot, "alpha", cfgFile);
    addProjectShellAllowed(projectRoot, "beta", cfgFile);
    const result = handleSlash("permissions", ["clear"], makeLoop(), { codeRoot: projectRoot });
    expect(result.info).toMatch(/about to drop 2 project allowlist/);
    expect(loadProjectShellAllowed(projectRoot, cfgFile)).toEqual(["alpha", "beta"]);
  });

  it("/permissions clear confirm wipes the project list", () => {
    const cfgFile = join(dir, ".reasonix", "config.json");
    addProjectShellAllowed(projectRoot, "alpha", cfgFile);
    addProjectShellAllowed(projectRoot, "beta", cfgFile);
    const result = handleSlash("permissions", ["clear", "confirm"], makeLoop(), {
      codeRoot: projectRoot,
    });
    expect(result.info).toMatch(/cleared 2 project allowlist/);
    expect(loadProjectShellAllowed(projectRoot, cfgFile)).toEqual([]);
  });

  it("mutating subcommands refuse without a codeRoot", () => {
    const r1 = handleSlash("permissions", ["add", "lint"], makeLoop(), {});
    expect(r1.info).toMatch(/only available inside `reasonix code`/);
    const r2 = handleSlash("permissions", ["remove", "lint"], makeLoop(), {});
    expect(r2.info).toMatch(/only available inside `reasonix code`/);
    const r3 = handleSlash("permissions", ["clear", "confirm"], makeLoop(), {});
    expect(r3.info).toMatch(/only available inside `reasonix code`/);
  });

  it("'perms' is registered as an alias for 'permissions'", () => {
    const result = handleSlash("perms", [], makeLoop(), {});
    expect(result.info).toMatch(/Builtin allowlist/);
  });

  it("unknown subcommand surfaces the usage block", () => {
    const result = handleSlash("permissions", ["wat"], makeLoop(), { codeRoot: projectRoot });
    expect(result.info).toMatch(/usage: \/permissions/);
  });
});
