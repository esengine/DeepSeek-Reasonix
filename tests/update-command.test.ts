/** reasonix update — pure planUpdate + orchestrator with every side effect mocked via test seams. */

import { describe, expect, it } from "vitest";
import { planUpdate, updateCommand } from "../src/cli/commands/update.js";
import { VERSION } from "../src/version.js";

describe("planUpdate", () => {
  it("up-to-date when current === latest", () => {
    const plan = planUpdate({ current: "0.4.22", latest: "0.4.22", installSource: "npm" });
    expect(plan.action).toBe("up-to-date");
    expect(plan.command).toBeUndefined();
  });

  it("newer-local when current > latest (dev build)", () => {
    const plan = planUpdate({ current: "0.5.0", latest: "0.4.22", installSource: "npm" });
    expect(plan.action).toBe("newer-local");
    expect(plan.command).toBeUndefined();
  });

  it("npx-hint when current < latest and running via npx", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "npx" });
    expect(plan.action).toBe("npx-hint");
    expect(plan.command).toBeUndefined();
    expect(plan.message).toContain("npx");
  });

  it("emits npm install -g for npm-installed binaries", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "npm" });
    expect(plan.action).toBe("run-install");
    expect(plan.command).toEqual(["npm", "install", "-g", "reasonix@latest"]);
  });

  it("pins npm to the install prefix when one was extracted (nvm/fnm robustness)", () => {
    const plan = planUpdate({
      current: "0.4.21",
      latest: "0.4.22",
      installSource: "npm",
      npmPrefix: "/Users/me/.nvm/versions/node/v22.11.0",
    });
    expect(plan.command).toEqual([
      "npm",
      "--prefix",
      "/Users/me/.nvm/versions/node/v22.11.0",
      "install",
      "-g",
      "reasonix@latest",
    ]);
  });

  it("emits bun add -g for bun-installed binaries", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "bun" });
    expect(plan.action).toBe("run-install");
    expect(plan.command).toEqual(["bun", "add", "-g", "reasonix"]);
    expect(plan.message).toContain("bun");
  });

  it("emits pnpm add -g for pnpm-installed binaries", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "pnpm" });
    expect(plan.action).toBe("run-install");
    expect(plan.command).toEqual(["pnpm", "add", "-g", "reasonix@latest"]);
  });

  it("emits yarn global add for yarn-installed binaries", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "yarn" });
    expect(plan.action).toBe("run-install");
    expect(plan.command).toEqual(["yarn", "global", "add", "reasonix@latest"]);
  });

  it("returns manual-hint when source cannot be detected — no silent npm fallback", () => {
    const plan = planUpdate({ current: "0.4.21", latest: "0.4.22", installSource: "unknown" });
    expect(plan.action).toBe("manual-hint");
    expect(plan.command).toBeUndefined();
    expect(plan.message).toContain("npm install -g reasonix@latest");
    expect(plan.message).toContain("bun add -g reasonix");
    expect(plan.message).toContain("pnpm add -g reasonix@latest");
    expect(plan.message).toContain("yarn global add reasonix@latest");
  });
});

describe("updateCommand", () => {
  function harness() {
    const output: string[] = [];
    let exitCode: number | undefined;
    const spawnCalls: string[][] = [];
    return {
      output,
      get exitCode() {
        return exitCode;
      },
      spawnCalls,
      write: (m: string) => {
        output.push(m);
      },
      exit: (c: number) => {
        exitCode = c;
      },
      spawnInstall: async (argv: string[]) => {
        spawnCalls.push(argv);
        return 0;
      },
    };
  }

  it("prints up-to-date and does NOT spawn when current === latest", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => VERSION,
      detectSource: () => "npm",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    expect(h.output.join("")).toContain("up to date");
    expect(h.spawnCalls).toHaveLength(0);
    expect(h.exitCode).toBeUndefined();
  });

  it("prints npx hint and does NOT spawn when running under npx", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "npx",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    const joined = h.output.join("");
    expect(joined).toContain("99.99.99");
    expect(joined).toContain("npx");
    expect(h.spawnCalls).toHaveLength(0);
  });

  it("spawns npm install -g when npm-installed and behind latest", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "npm",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    expect(h.spawnCalls).toEqual([["npm", "install", "-g", "reasonix@latest"]]);
    expect(h.exitCode).toBeUndefined();
  });

  it("spawns bun add -g when bun-installed", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "bun",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    expect(h.spawnCalls).toEqual([["bun", "add", "-g", "reasonix"]]);
  });

  it("refuses with manual hint and exits 1 when source cannot be detected", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "unknown",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    const joined = h.output.join("");
    expect(joined).toContain("could not be determined");
    expect(joined).toContain("npm install -g reasonix@latest");
    expect(joined).toContain("bun add -g reasonix");
    expect(h.spawnCalls).toHaveLength(0);
    expect(h.exitCode).toBe(1);
  });

  it("--dry-run prints the command but does not spawn", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "npm",
      detectPrefix: () => null,
      dryRun: true,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    expect(h.spawnCalls).toHaveLength(0);
    expect(h.output.join("")).toContain("(dry run)");
  });

  it("exits non-zero when the registry is unreachable", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => null,
      detectSource: () => "npm",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: h.spawnInstall,
    });
    expect(h.output.join("")).toContain("could not reach");
    expect(h.exitCode).toBe(1);
  });

  it("surfaces non-zero installer exit via the exit seam", async () => {
    const h = harness();
    await updateCommand({
      fetchLatest: async () => "99.99.99",
      detectSource: () => "npm",
      detectPrefix: () => null,
      write: h.write,
      exit: h.exit,
      spawnInstall: async () => 127,
    });
    expect(h.exitCode).toBe(127);
    expect(h.output.join("")).toContain("did not complete");
  });
});
