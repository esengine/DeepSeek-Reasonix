import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import type { DetectedTestCommand } from "./types.js";

export function detectTestCommand(rootDir: string): DetectedTestCommand | null {
  const node = detectNodeTestCommand(rootDir);
  if (node) return node;
  if (existsSync(join(rootDir, "pytest.ini")) || hasPytestInPyproject(rootDir)) {
    return {
      kind: "python",
      command: "pytest",
      args: [],
      display: "pytest",
      reason: "pytest configuration detected",
    };
  }
  if (existsSync(join(rootDir, "go.mod"))) {
    return {
      kind: "go",
      command: "go",
      args: ["test", "./..."],
      display: "go test ./...",
      reason: "go.mod detected",
    };
  }
  if (existsSync(join(rootDir, "Cargo.toml"))) {
    return {
      kind: "rust",
      command: "cargo",
      args: ["test"],
      display: "cargo test",
      reason: "Cargo.toml detected",
    };
  }
  return null;
}

function detectNodeTestCommand(rootDir: string): DetectedTestCommand | null {
  const packagePath = join(rootDir, "package.json");
  if (!existsSync(packagePath)) return null;
  try {
    const raw = JSON.parse(readFileSync(packagePath, "utf8")) as {
      scripts?: Record<string, unknown>;
      packageManager?: unknown;
    };
    const script = raw.scripts?.test;
    if (typeof script !== "string" || !script.trim() || isDefaultNpmNoTest(script)) return null;
    const usePnpm =
      existsSync(join(rootDir, "pnpm-lock.yaml")) ||
      (typeof raw.packageManager === "string" && raw.packageManager.startsWith("pnpm@"));
    return {
      kind: "node",
      command: usePnpm ? "pnpm" : "npm",
      args: ["test"],
      display: usePnpm ? "pnpm test" : "npm test",
      reason: "package.json test script detected",
    };
  } catch {
    return null;
  }
}

function isDefaultNpmNoTest(script: string): boolean {
  return /no test specified/i.test(script) && /exit\s+1/.test(script);
}

function hasPytestInPyproject(rootDir: string): boolean {
  for (const name of ["pyproject.toml", "setup.cfg"]) {
    const path = join(rootDir, name);
    if (!existsSync(path)) continue;
    try {
      const body = readFileSync(path, "utf8");
      if (/\bpytest\b|\[tool\.pytest/i.test(body)) return true;
    } catch {
      /* ignore unreadable config */
    }
  }
  return false;
}
