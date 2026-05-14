/** Bare `reasonix` routing — project-like cwd should launch code mode, explicit `chat` must stay chat. */

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { writeConfig } from "../src/config.js";

const codeCommand = vi.fn(async () => {});
const chatCommand = vi.fn(async () => {});
const setupCommand = vi.fn(async () => {});

vi.mock("../src/cli/commands/code.js", () => ({ codeCommand }));
vi.mock("../src/cli/commands/chat.js", () => ({ chatCommand }));
vi.mock("../src/cli/commands/setup.js", () => ({ setupCommand }));

async function importCli(argv: string[]) {
  vi.resetModules();
  process.argv = ["node", "src/cli/index.ts", ...argv];
  await import("../src/cli/index.ts");
}

describe("bare CLI routing", () => {
  let home: string;
  let cwd: string;
  const origHome = process.env.HOME;
  const origUserProfile = process.env.USERPROFILE;
  const origArgv = process.argv;
  const origCwd = process.cwd();
  let stderr: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-cli-home-"));
    cwd = mkdtempSync(join(tmpdir(), "reasonix-cli-cwd-"));
    process.env.HOME = home;
    process.env.USERPROFILE = home;
    process.chdir(cwd);
    codeCommand.mockClear();
    chatCommand.mockClear();
    setupCommand.mockClear();
    stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });

  afterEach(() => {
    stderr.mockRestore();
    process.chdir(origCwd);
    process.argv = origArgv;
    rmSync(home, { recursive: true, force: true });
    rmSync(cwd, { recursive: true, force: true });
    if (origHome === undefined) {
      // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
      delete process.env.HOME;
    } else {
      process.env.HOME = origHome;
    }
    if (origUserProfile === undefined) {
      // biome-ignore lint/performance/noDelete: env restoration needs absence, not "undefined"
      delete process.env.USERPROFILE;
    } else {
      process.env.USERPROFILE = origUserProfile;
    }
  });

  it("routes bare reasonix in a .git directory to code mode rooted at cwd", async () => {
    writeConfig({ setupCompleted: true }, join(home, ".reasonix", "config.json"));
    mkdirSync(join(cwd, ".git"));

    await importCli([]);

    await vi.waitFor(() => expect(codeCommand).toHaveBeenCalledWith({ dir: cwd }));
    expect(chatCommand).not.toHaveBeenCalled();
  });

  it("routes bare reasonix in a non-project directory to chat mode and prints the filesystem hint", async () => {
    writeConfig({ setupCompleted: true }, join(home, ".reasonix", "config.json"));

    await importCli([]);

    await vi.waitFor(() => expect(chatCommand).toHaveBeenCalled());
    expect(codeCommand).not.toHaveBeenCalled();
    expect(stderr.mock.calls.map((call) => String(call[0])).join("")).toContain(
      "chat mode (no filesystem tools). Run `reasonix code` to work on files in this folder.",
    );
  });

  it("routes bare reasonix in a recent workspace to code mode", async () => {
    writeConfig(
      { setupCompleted: true, recentWorkspaces: [cwd] },
      join(home, ".reasonix", "config.json"),
    );

    await importCli([]);

    await vi.waitFor(() => expect(codeCommand).toHaveBeenCalledWith({ dir: cwd }));
    expect(chatCommand).not.toHaveBeenCalled();
  });

  it("keeps explicit reasonix chat in chat mode even inside a project", async () => {
    writeConfig({ setupCompleted: true }, join(home, ".reasonix", "config.json"));
    writeFileSync(join(cwd, "package.json"), "{}\n", "utf8");

    await importCli(["chat"]);

    await vi.waitFor(() => expect(chatCommand).toHaveBeenCalled());
    expect(codeCommand).not.toHaveBeenCalled();
  });

  it("keeps first-run bare reasonix on the setup wizard", async () => {
    writeConfig({ setupCompleted: false }, join(home, ".reasonix", "config.json"));
    mkdirSync(join(cwd, ".git"));

    await importCli([]);

    await vi.waitFor(() => expect(setupCommand).toHaveBeenCalledWith({ forceKeyStep: true }));
    expect(codeCommand).not.toHaveBeenCalled();
    expect(chatCommand).not.toHaveBeenCalled();
  });
});
