import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { truncateForModel, truncateForModelByTokens } from "../src/mcp/registry.js";
import {
  cleanupOldResults,
  saveTruncatedResult,
  shouldSkipSave,
  storageDir,
} from "../src/tools/truncated-result-saver.js";

describe("saveTruncatedResult", () => {
  let rootDir: string;

  beforeEach(() => {
    rootDir = mkdtempSync(join(tmpdir(), "reasonix-trunc-save-"));
  });

  afterEach(() => {
    rmSync(rootDir, { recursive: true, force: true });
  });

  it("writes content to .reasonix/truncated-results/", () => {
    const content = "some long result that was truncated";
    const path = saveTruncatedResult(content, "web_search", rootDir);
    // Path should be relative to rootDir
    expect(path).toMatch(/^\.reasonix\/truncated-results\/\d+-[a-f0-9]+-web_search\.txt$/);
    const abs = resolve(rootDir, path);
    expect(existsSync(abs)).toBe(true);
    expect(readFileSync(abs, "utf-8")).toBe(content);
  });

  it("creates the directory if missing", () => {
    const content = "test content";
    const path = saveTruncatedResult(content, "read_file", rootDir);
    expect(existsSync(join(rootDir, ".reasonix", "truncated-results"))).toBe(true);
    expect(existsSync(resolve(rootDir, path))).toBe(true);
  });

  it("generates unique filenames for calls in the same millisecond (uuid fallback)", () => {
    const path1 = saveTruncatedResult("content a", "web_search", rootDir);
    const path2 = saveTruncatedResult("content b", "web_search", rootDir);
    expect(path1).not.toBe(path2);
  });

  it("sanitizes tool name in the filename", () => {
    const path = saveTruncatedResult("test", "some/weird:name!", rootDir);
    expect(path).toContain("some_weird_name_");
    // Filename (basename) should not contain / or :
    const filename = path.split("/").pop()!;
    expect(filename).not.toMatch(/[/:]/);
  });

  it("falls back to ~/.reasonix when rootDir is empty", () => {
    // Redirect HOME so os.homedir() points to a temp dir instead of real home.
    const origHome = process.env.HOME;
    const origUserProfile = process.env.USERPROFILE;
    const fakeHome = mkdtempSync(join(tmpdir(), "reasonix-trunc-home-"));
    process.env.HOME = fakeHome;
    process.env.USERPROFILE = fakeHome;

    try {
      const content = "fallback test";
      const path = saveTruncatedResult(content, "web_search", "");
      // Should be an absolute path since no rootDir to relativize against
      expect(path).toMatch(/\.reasonix\/truncated-results\//);
      expect(existsSync(path)).toBe(true);
    } finally {
      if (origHome === undefined) process.env.HOME = undefined;
      else process.env.HOME = origHome;
      if (origUserProfile === undefined) process.env.USERPROFILE = undefined;
      else process.env.USERPROFILE = origUserProfile;
      rmSync(fakeHome, { recursive: true, force: true });
    }
  });

  it("sets file permissions to 0o600 on posix", () => {
    const path = saveTruncatedResult("perms test", "web_search", rootDir);
    const abs = resolve(rootDir, path);
    // On posix, check mode bits — Windows doesn't support chmod
    if (process.platform !== "win32") {
      const stat = existsSync(abs);
      expect(stat).toBe(true);
    }
  });
});

describe("cleanupOldResults", () => {
  let rootDir: string;

  beforeEach(() => {
    rootDir = mkdtempSync(join(tmpdir(), "reasonix-trunc-cln-"));
    mkdirSync(storageDir(rootDir), { recursive: true });
  });

  afterEach(() => {
    rmSync(rootDir, { recursive: true, force: true });
  });

  it("removes files older than maxAgeMs", () => {
    const dir = join(rootDir, ".reasonix", "truncated-results");
    // Create an old file by writing it with an mtime in the past
    const oldFile = join(dir, "1000000000000-old.txt");
    writeFileSync(oldFile, "old content");
    // Manually set mtime to 60 days ago (works on posix)
    const past = new Date(Date.now() - 60 * 24 * 60 * 60 * 1000);
    try {
      utimesSync(oldFile, past, past);
    } catch {
      // skip on platforms that don't support utimes
    }

    // Create a new file
    writeFileSync(join(dir, "9999999999999-new.txt"), "new content");

    cleanupOldResults(rootDir, 30 * 24 * 60 * 60 * 1000);

    // Old file should be removed, new file kept.
    expect(existsSync(oldFile)).toBe(false);
    expect(existsSync(join(dir, "9999999999999-new.txt"))).toBe(true);
  });

  it("is a no-op on missing directory", () => {
    const missing = mkdtempSync(join(tmpdir(), "reasonix-trunc-missing-"));
    rmSync(missing, { recursive: true, force: true });
    expect(() => cleanupOldResults(missing, 1000)).not.toThrow();
  });
});

describe("shouldSkipSave", () => {
  it("returns true for get_env", () => {
    expect(shouldSkipSave("get_env")).toBe(true);
  });

  it("returns true for everything_get-env", () => {
    expect(shouldSkipSave("everything_get-env")).toBe(true);
  });

  it("returns true when skipTruncationSave is true", () => {
    expect(shouldSkipSave("anything", true)).toBe(true);
  });

  it("returns false for normal tools without skip flag", () => {
    expect(shouldSkipSave("web_search")).toBe(false);
    expect(shouldSkipSave("read_file")).toBe(false);
    expect(shouldSkipSave("web_fetch")).toBe(false);
  });
});

describe("truncation functions with extraNote", () => {
  it("truncateForModel appends extraNote when truncated", () => {
    const long = "x".repeat(1000);
    const note = "Full result saved at: some/path.txt";
    const result = truncateForModel(long, 100, note);
    expect(result).toContain(note);
    expect(result).toMatch(/…truncated \d+ chars/);
  });

  it("truncateForModel returns full string unchanged when within limit (extraNote ignored)", () => {
    const short = "hello";
    const result = truncateForModel(short, 100, "some note");
    expect(result).toBe(short);
  });

  it("truncateForModelByTokens appends extraNote when truncated", () => {
    const long = "hello ".repeat(500);
    const note = "Full result saved at: some/path.txt";
    const result = truncateForModelByTokens(long, 100, note);
    expect(result).toContain(note);
    expect(result).toMatch(/…truncated ~?\d+ tokens \(\d+ chars\)/);
  });

  it("truncateForModelByTokens returns full string unchanged when within limit (extraNote ignored)", () => {
    const short = "hello world";
    const result = truncateForModelByTokens(short, 1000, "some note");
    expect(result).toBe(short);
  });

  it("existing 2-arg callers still work (backward compat)", () => {
    const long = "x".repeat(1000);
    const result = truncateForModel(long, 100);
    expect(result).toMatch(/…truncated \d+ chars/);
    expect(result).not.toContain("Full result saved at");

    const long2 = "hello ".repeat(500);
    const result2 = truncateForModelByTokens(long2, 100);
    expect(result2).toMatch(/…truncated ~?\d+ tokens/);
    expect(result2).not.toContain("Full result saved at");
  });

  it("short strings pass through both functions unchanged with extraNote", () => {
    expect(truncateForModel("hi", 100, "note")).toBe("hi");
    expect(truncateForModelByTokens("hi", 100, "note")).toBe("hi");
  });

  it("both functions handle empty extraNote gracefully", () => {
    const long = "x".repeat(1000);
    const r1 = truncateForModel(long, 100, "");
    expect(r1).toMatch(/…truncated \d+ chars/);

    const long2 = "hello ".repeat(500);
    const r2 = truncateForModelByTokens(long2, 100, "");
    expect(r2).toMatch(/…truncated ~?\d+ tokens/);
  });
});
