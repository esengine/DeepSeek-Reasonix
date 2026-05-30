import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  loadSavedWorkflows,
  saveWorkflowScript,
  savedWorkflowPath,
} from "../src/workflow/saved.js";

const script = `export const meta = { name: "audit", description: "Audit" }\nreturn true\n`;

describe("saved workflows", () => {
  let root: string;
  let home: string;

  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-wf-root-"));
    home = mkdtempSync(join(tmpdir(), "reasonix-wf-home-"));
  });

  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
    rmSync(home, { recursive: true, force: true });
  });

  it("saves project workflows under .reasonix/workflows", () => {
    const out = saveWorkflowScript({
      rootDir: root,
      homeDir: home,
      target: "project",
      name: "audit",
      script,
    });

    expect(out.path).toBe(join(root, ".reasonix", "workflows", "audit.js"));
    expect(readFileSync(out.path, "utf8")).toBe(script);
  });

  it("saves user workflows under ~/.reasonix/workflows", () => {
    const out = saveWorkflowScript({
      rootDir: root,
      homeDir: home,
      target: "user",
      name: "audit",
      script,
    });

    expect(out.path).toBe(join(home, ".reasonix", "workflows", "audit.js"));
    expect(readFileSync(out.path, "utf8")).toBe(script);
  });

  it("rejects unsafe workflow names", () => {
    expect(() =>
      savedWorkflowPath({
        rootDir: root,
        homeDir: home,
        target: "project",
        name: "../bad",
      }),
    ).toThrow(/workflow name/i);
  });

  it("loads project workflows before user workflows when names collide", () => {
    mkdirSync(join(home, ".reasonix", "workflows"), { recursive: true });
    mkdirSync(join(root, ".reasonix", "workflows"), { recursive: true });
    writeFileSync(
      join(home, ".reasonix", "workflows", "audit.js"),
      script.replace("Audit", "User"),
    );
    writeFileSync(
      join(root, ".reasonix", "workflows", "audit.js"),
      script.replace("Audit", "Project"),
    );

    const loaded = loadSavedWorkflows({ rootDir: root, homeDir: home });

    expect(loaded).toHaveLength(1);
    expect(loaded[0]).toMatchObject({ name: "audit", scope: "project" });
    expect(loaded[0]?.script).toContain("Project");
  });
});
