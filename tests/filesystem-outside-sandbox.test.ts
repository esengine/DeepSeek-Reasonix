import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { loadProjectPathAllowed, writeConfig } from "../src/config.js";
import { PauseGate } from "../src/core/pause-gate.js";
import { ToolRegistry } from "../src/tools.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";

describe("filesystem outside-sandbox gate (#684)", () => {
  let root: string;
  let outside: string;
  let configDir: string;
  let configPath: string;
  let tools: ToolRegistry;
  let gate: PauseGate;
  let gateRequests: Array<{ kind: string; payload: unknown }>;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-sandbox-"));
    outside = await mkdtemp(join(tmpdir(), "reasonix-outside-"));
    configDir = await mkdtemp(join(tmpdir(), "reasonix-cfg-"));
    configPath = join(configDir, "config.json");
    writeConfig({}, configPath);
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root });
    await fs.writeFile(join(outside, "secret.txt"), "outside content");
    gateRequests = [];
    gate = new PauseGate();
    gate.on((req) => {
      gateRequests.push({ kind: req.kind, payload: req.payload });
    });
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
    await rm(outside, { recursive: true, force: true });
    await rm(configDir, { recursive: true, force: true });
  });

  it("dispatches a path_access gate request when a system absolute path escapes the sandbox", async () => {
    const target = join(outside, "secret.txt");
    const call = tools.dispatch("read_file", { path: target }, { confirmationGate: gate });
    await new Promise((r) => setTimeout(r, 5));
    expect(gateRequests).toHaveLength(1);
    expect(gateRequests[0]?.kind).toBe("path_access");
    const payload = gateRequests[0]?.payload as {
      path: string;
      intent: string;
      toolName: string;
    };
    expect(payload.intent).toBe("read");
    expect(payload.toolName).toBe("read_file");
    expect(payload.path).toBe(target);
    const current = gate.current;
    expect(current).not.toBeNull();
    gate.resolve(current!.id, { type: "run_once" });
    const result = await call;
    expect(result).toContain("outside content");
  });

  it("throws a user-denied error when the gate verdict is deny", async () => {
    const target = join(outside, "secret.txt");
    const call = tools.dispatch("read_file", { path: target }, { confirmationGate: gate });
    await new Promise((r) => setTimeout(r, 5));
    const current = gate.current;
    expect(current).not.toBeNull();
    gate.resolve(current!.id, { type: "deny", denyContext: "not on the contract" });
    const result = await call;
    expect(result).toMatch(/user denied/);
    expect(result).toMatch(/not on the contract/);
  });

  it("run_once approval covers a follow-up access in the same directory without re-prompting", async () => {
    await fs.writeFile(join(outside, "second.txt"), "second");
    const first = tools.dispatch(
      "read_file",
      { path: join(outside, "secret.txt") },
      { confirmationGate: gate },
    );
    await new Promise((r) => setTimeout(r, 5));
    gate.resolve(gate.current!.id, { type: "run_once" });
    await first;
    const second = await tools.dispatch(
      "read_file",
      { path: join(outside, "second.txt") },
      { confirmationGate: gate },
    );
    expect(second).toContain("second");
    expect(gateRequests).toHaveLength(1);
  });

  it("rejects relative paths that escape (only system absolutes are gate-eligible)", async () => {
    const result = await tools.dispatch(
      "read_file",
      { path: "../escape" },
      { confirmationGate: gate },
    );
    expect(result).toMatch(/escapes sandbox root/);
    expect(gateRequests).toHaveLength(0);
  });

  it("preserves the leading-slash sandbox convention for paths like /src/foo.ts", async () => {
    await fs.mkdir(join(root, "src"), { recursive: true });
    await fs.writeFile(join(root, "src", "foo.ts"), "inside");
    const result = await tools.dispatch(
      "read_file",
      { path: "/src/foo.ts" },
      { confirmationGate: gate },
    );
    expect(result).toContain("inside");
    expect(gateRequests).toHaveLength(0);
  });

  it("write_file outside the sandbox routes through the gate with intent=write", async () => {
    const target = join(outside, "written.txt");
    const call = tools.dispatch(
      "write_file",
      { path: target, content: "from-test" },
      { confirmationGate: gate },
    );
    await new Promise((r) => setTimeout(r, 5));
    expect(gateRequests).toHaveLength(1);
    const payload = gateRequests[0]?.payload as { intent: string; toolName: string };
    expect(payload.intent).toBe("write");
    expect(payload.toolName).toBe("write_file");
    gate.resolve(gate.current!.id, { type: "run_once" });
    await call;
    const onDisk = await fs.readFile(target, "utf8");
    expect(onDisk).toBe("from-test");
  });

  it("Windows drive-letter paths route to the gate (covers the looksAbsoluteSystemPath fallback)", async () => {
    // This path doesn't exist on POSIX but the resolver/check fires regardless.
    if (process.platform !== "win32") return;
    const call = tools.dispatch(
      "read_file",
      { path: "C:\\Windows\\System32\\drivers\\etc\\hosts" },
      { confirmationGate: gate },
    );
    await new Promise((r) => setTimeout(r, 5));
    expect(gateRequests).toHaveLength(1);
    gate.resolve(gate.current!.id, { type: "deny" });
    const result = await call;
    expect(result).toMatch(/user denied/);
  });

  it("loadProjectPathAllowed is independently exposed for the slash-permissions UI", () => {
    expect(loadProjectPathAllowed(root, configPath)).toEqual([]);
  });
});
