import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, realpathSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { claimShellInstance } from "./singleInstance.js";

test("single-instance lock uses the selected canonical data home before acquisition", () => {
  const root = mkdtempSync(join(tmpdir(), "reasonix-instance-"));
  try {
    const profiles = new Set<string>();
    const host = () => {
      let profile = "";
      return {
        setPath: (_name: "userData", path: string) => { profile = path; },
        requestSingleInstanceLock: () => {
          assert.notEqual(profile, "", "userData must precede the lock request");
          if (profiles.has(profile)) return false;
          profiles.add(profile);
          return true;
        },
      };
    };
    assert.equal(claimShellInstance(host(), join(root, "a"), false), true);
    assert.equal(claimShellInstance(host(), join(root, "b"), false), true, "different homes coexist");
    assert.equal(claimShellInstance(host(), join(root, "a"), false), false, "same home reuses its instance");
    const alias = join(root, "alias");
    symlinkSync(join(root, "a"), alias, process.platform === "win32" ? "junction" : "dir");
    assert.equal(claimShellInstance(host(), alias, false), false, "a symlink is the same data home");
    mkdirSync(join(root, "dev"));
    let seen = "";
    assert.equal(claimShellInstance({ setPath: (_name, path) => { seen = path; }, requestSingleInstanceLock: () => { throw new Error("dev must not lock"); } }, join(root, "dev"), true), true);
    assert.equal(seen, realpathSync.native(join(root, "dev", "desktop-shell")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
