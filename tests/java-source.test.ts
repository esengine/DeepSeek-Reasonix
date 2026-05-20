import { execFile } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, sep } from "node:path";
import { deflateRawSync } from "node:zlib";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ClassSourceFinder } from "../src/java/class-source-finder.js";
import { listJarEntries, readJarEntry } from "../src/java/zip-reader.js";
import { ToolRegistry } from "../src/tools.js";
import { registerJavaSourceTool } from "../src/tools/java-source.js";

// Default throwing implementation, reused for mock resets in afterEach hooks.
// hoisted: vitest lifts vi.mock factories above all imports — the variable
// must be declared inside vi.hoisted so it's visible when the factory runs.
const { defaultExecFileImpl } = vi.hoisted(() => {
  const fn = (_cmd: unknown, _args: unknown, _opts: unknown, cb: unknown) => {
    if (typeof cb === "function") {
      (cb as (err: Error | null, stdout: string, stderr: string) => void)(
        new Error("execFile not configured for this test — mock it in beforeEach"),
        "",
        "",
      );
    }
  };
  return { defaultExecFileImpl: fn };
});

// Make execFile return errors by default — individual describe blocks
// override this when they need javap to succeed.
vi.mock("node:child_process", () => ({
  execFile: vi.fn(defaultExecFileImpl),
}));

// ── helpers ────────────────────────────────────────────────────────────────

let tmpDirs: string[] = [];

async function tmpDir(): Promise<string> {
  const d = await mkdtemp(join(tmpdir(), "reasonix-java-test-"));
  tmpDirs.push(d);
  return d;
}

afterEach(async () => {
  for (const d of tmpDirs) {
    await rm(d, { recursive: true, force: true }).catch(() => {});
  }
  tmpDirs = [];
});

/** Table-based CRC32 — no external deps needed. */
function crc32(buf: Buffer): number {
  let crc = 0xffffffff;
  for (let i = 0; i < buf.length; i++) {
    crc ^= buf[i]!;
    for (let j = 0; j < 8; j++) {
      crc = crc & 1 ? (crc >>> 1) ^ 0xedb88320 : crc >>> 1;
    }
  }
  return (crc ^ 0xffffffff) >>> 0;
}

// Create a minimal valid ZIP file with one or more entries.
// compression: 0 = STORED (default), 8 = DEFLATED.
function createZip(entries: Array<{ name: string; data: Buffer; compression?: number }>): Buffer {
  const localHeaders: Buffer[] = [];
  const centralEntries: Buffer[] = [];
  let dataOffset = 0;

  for (const entry of entries) {
    const compressionMethod = entry.compression ?? 0;
    const nameBytes = Buffer.from(entry.name, "utf8");
    const data = entry.data;
    const crc = crc32(data);

    let compressedData: Buffer;
    if (compressionMethod === 0) {
      compressedData = data;
    } else if (compressionMethod === 8) {
      compressedData = deflateRawSync(data);
    } else {
      throw new Error(`Unsupported compression method: ${compressionMethod}`);
    }

    const compressedSize = compressedData.length;
    const uncompressedSize = data.length;

    // Local file header
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0); // signature
    local.writeUInt16LE(20, 4); // version needed
    local.writeUInt16LE(0, 6); // flags
    local.writeUInt16LE(compressionMethod, 8); // compression method
    local.writeUInt16LE(0, 10); // mod time
    local.writeUInt16LE(0, 12); // mod date
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(compressedSize, 18); // compressed size
    local.writeUInt32LE(uncompressedSize, 22); // uncompressed size
    local.writeUInt16LE(nameBytes.length, 26);
    local.writeUInt16LE(0, 28); // extra field length

    localHeaders.push(Buffer.concat([local, nameBytes, compressedData]));

    // Central directory entry
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0); // signature
    central.writeUInt16LE(20, 4); // version made by
    central.writeUInt16LE(20, 6); // version needed
    central.writeUInt16LE(0, 8); // flags
    central.writeUInt16LE(compressionMethod, 10); // compression
    central.writeUInt16LE(0, 12); // mod time
    central.writeUInt16LE(0, 14); // mod date
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(compressedSize, 20); // compressed size
    central.writeUInt32LE(uncompressedSize, 24); // uncompressed size
    central.writeUInt16LE(nameBytes.length, 28);
    central.writeUInt16LE(0, 30); // extra field length
    central.writeUInt16LE(0, 32); // comment length
    central.writeUInt16LE(0, 34); // disk number start
    central.writeUInt16LE(0, 36); // internal attrs
    central.writeUInt32LE(0, 38); // external attrs
    central.writeUInt32LE(dataOffset, 42); // local header offset

    centralEntries.push(Buffer.concat([central, nameBytes]));
    dataOffset += 30 + nameBytes.length + compressedData.length;
  }

  const centralDir = Buffer.concat(centralEntries);
  const centralDirSize = centralDir.length;
  const centralDirOffset = localHeaders.reduce((s, b) => s + b.length, 0);

  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0); // signature
  eocd.writeUInt16LE(0, 4); // disk number
  eocd.writeUInt16LE(0, 6); // disk with central dir
  eocd.writeUInt16LE(entries.length, 8); // entries on this disk
  eocd.writeUInt16LE(entries.length, 10); // total entries
  eocd.writeUInt32LE(centralDirSize, 12); // size of central directory
  eocd.writeUInt32LE(centralDirOffset, 16); // offset of start of central directory
  eocd.writeUInt16LE(0, 20); // comment length

  return Buffer.concat([...localHeaders, centralDir, eocd]);
}

// ── zip-reader tests ─────────────────────────────────────────────────────────

describe("readJarEntry", () => {
  it("reads a stored entry from a valid zip", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "test.jar");
    const content = Buffer.from("hello world");
    const zip = createZip([{ name: "hello.txt", data: content }]);
    writeFileSync(zipPath, zip);

    const result = readJarEntry(zipPath, "hello.txt");
    expect(result).not.toBeNull();
    expect(result!.fileName).toBe("hello.txt");
    expect(result!.data.toString()).toBe("hello world");
  });

  it("returns null for a missing entry", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "test.jar");
    const zip = createZip([{ name: "present.class", data: Buffer.from("fake") }]);
    writeFileSync(zipPath, zip);

    const result = readJarEntry(zipPath, "missing.class");
    expect(result).toBeNull();
  });

  it("reads a class entry from a jar-style path", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "guava.jar");
    const classContent = Buffer.from("fake bytecode");
    const zip = createZip([{ name: "com/google/common/collect/Lists.class", data: classContent }]);
    writeFileSync(zipPath, zip);

    const result = readJarEntry(zipPath, "com/google/common/collect/Lists.class");
    expect(result).not.toBeNull();
    expect(result!.fileName).toBe("com/google/common/collect/Lists.class");
  });

  it("handles multiple entries and reads the right one", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "multi.jar");
    const zip = createZip([
      { name: "META-INF/MANIFEST.MF", data: Buffer.from("Manifest-Version: 1.0\n") },
      { name: "com/example/Foo.class", data: Buffer.from("foo bytes") },
      { name: "com/example/Bar.class", data: Buffer.from("bar bytes") },
    ]);
    writeFileSync(zipPath, zip);

    const foo = readJarEntry(zipPath, "com/example/Foo.class");
    expect(foo!.data.toString()).toBe("foo bytes");

    const bar = readJarEntry(zipPath, "com/example/Bar.class");
    expect(bar!.data.toString()).toBe("bar bytes");
  });
});

describe("listJarEntries", () => {
  it("lists all entries in a zip", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "test.jar");
    const zip = createZip([
      { name: "a.class", data: Buffer.from("a") },
      { name: "b.class", data: Buffer.from("b") },
      { name: "c.class", data: Buffer.from("c") },
    ]);
    writeFileSync(zipPath, zip);

    const entries = listJarEntries(zipPath);
    expect(entries).toEqual(["a.class", "b.class", "c.class"]);
  });

  it("returns empty array for empty zip", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "empty.jar");
    const zip = createZip([]);
    writeFileSync(zipPath, zip);

    const entries = listJarEntries(zipPath);
    expect(entries).toEqual([]);
  });
});

describe("readJarEntry — error handling", () => {
  it("throws on non-existent file", () => {
    expect(() => readJarEntry("/nonexistent/path.jar", "x")).toThrow();
  });

  it("throws on corrupt data (not a zip)", async () => {
    const dir = await tmpDir();
    const f = join(dir, "garbage.bin");
    writeFileSync(f, Buffer.from("not a zip file"));
    expect(() => readJarEntry(f, "x")).toThrow();
  });
});

// ── ClassSourceFinder tests ──────────────────────────────────────────────────

describe("ClassSourceFinder.defaultRepoPaths", () => {
  it("returns an array (may be empty if no repo dirs exist)", () => {
    const paths = ClassSourceFinder.defaultRepoPaths();
    expect(Array.isArray(paths)).toBe(true);
    for (const p of paths) {
      expect(typeof p).toBe("string");
    }
  });
});

describe("ClassSourceFinder — project search", () => {
  it("finds a .java file in the project tree", async () => {
    const root = await tmpDir();
    const pkgDir = join(root, "src", "com", "example");
    mkdirSync(pkgDir, { recursive: true });
    const javaSrc = "package com.example;\npublic class StringKit {}\n";
    writeFileSync(join(pkgDir, "StringKit.java"), javaSrc);

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSource("com.example.StringKit");

    expect(result.found).toBe(true);
    if (result.found) {
      expect(result.method).toBe("project");
      expect(result.source).toContain("public class StringKit");
      expect(result.sourcePath).toContain("StringKit.java");
    }
  });

  it("returns not-found when no .java file matches", async () => {
    const root = await tmpDir();
    mkdirSync(join(root, "src"), { recursive: true });
    writeFileSync(join(root, "src", "other.ts"), "export const x = 1;\n");

    const finder = new ClassSourceFinder({ projectRoot: root, repoPaths: [] });
    const result = await finder.findSource("com.example.Missing");

    expect(result.found).toBe(false);
  }, 30_000);

  it("skips common non-source directories", async () => {
    const root = await tmpDir();
    mkdirSync(join(root, "target", "classes"), { recursive: true });
    writeFileSync(join(root, "target", "classes", "MyClass.java"), "// should be ignored");

    const finder = new ClassSourceFinder({ projectRoot: root, repoPaths: [] });
    const result = await finder.findSource("MyClass");
    expect(result.found).toBe(false);
  }, 30_000);

  it("accepts jarKeyword filter (does not change project search result)", async () => {
    const root = await tmpDir();
    const pkgDir = join(root, "src");
    mkdirSync(pkgDir, { recursive: true });
    writeFileSync(join(pkgDir, "MyClass.java"), "public class MyClass {}");

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSource("MyClass", { jarKeyword: "spring" });

    // jarKeyword only filters the jar-scan step; project search should still match
    expect(result.found).toBe(true);
  });
});

describe("ClassSourceFinder — direct jar path", () => {
  it("reads the class entry from the jar (decompile step requires javap on PATH)", async () => {
    const root = await tmpDir();
    const jarPath = join(root, "my-lib.jar");
    const classContent = Buffer.from("fake class data");
    const zip = createZip([{ name: "com/example/Util.class", data: classContent }]);
    writeFileSync(jarPath, zip);

    // Test that the jar entry can be read correctly by the zip reader
    const entry = readJarEntry(jarPath, "com/example/Util.class");
    expect(entry).not.toBeNull();
    expect(entry!.data.toString()).toBe("fake class data");
  });

  it("returns not-found for non-existent entry", async () => {
    const root = await tmpDir();
    const jarPath = join(root, "empty.jar");
    const zip = createZip([{ name: "other/Class.class", data: Buffer.from("x") }]);
    writeFileSync(jarPath, zip);

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSourceInJar("com.example.Missing", jarPath);

    expect(result.found).toBe(false);
  });

  it("returns not-found for non-existent jar file", async () => {
    const finder = new ClassSourceFinder({ projectRoot: "/tmp" });
    const result = await finder.findSourceInJar("com.example.X", "/nonexistent/jar.jar");

    expect(result.found).toBe(false);
  });
});

describe("ClassSourceFinder — AbortSignal", () => {
  it("throws on aborted signal", async () => {
    const ctrl = new AbortController();
    ctrl.abort();
    const finder = new ClassSourceFinder({ projectRoot: "/tmp", signal: ctrl.signal });

    await expect(finder.findSource("com.example.X")).rejects.toThrow("Aborted");
  });
});

// ── zip-reader: DEFLATED compression ─────────────────────────────────────────

describe("readJarEntry — DEFLATED compression", () => {
  it("reads a DEFLATED entry from a zip", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "deflated.jar");
    const content = Buffer.from("hello deflated world");
    const zip = createZip([{ name: "hello.txt", data: content, compression: 8 }]);
    writeFileSync(zipPath, zip);

    const result = readJarEntry(zipPath, "hello.txt");
    expect(result).not.toBeNull();
    expect(result!.fileName).toBe("hello.txt");
    expect(result!.data.toString()).toBe("hello deflated world");
  });

  it("handles mixed STORED and DEFLATED entries", async () => {
    const dir = await tmpDir();
    const zipPath = join(dir, "mixed.jar");
    const zip = createZip([
      { name: "stored.txt", data: Buffer.from("stored content") },
      {
        name: "deflated.txt",
        data: Buffer.from("deflated content"),
        compression: 8,
      },
      { name: "another.txt", data: Buffer.from("more content") },
    ]);
    writeFileSync(zipPath, zip);

    const stored = readJarEntry(zipPath, "stored.txt");
    expect(stored!.data.toString()).toBe("stored content");

    const deflated = readJarEntry(zipPath, "deflated.txt");
    expect(deflated!.data.toString()).toBe("deflated content");

    const another = readJarEntry(zipPath, "another.txt");
    expect(another!.data.toString()).toBe("more content");
  });
});

// ── ClassSourceFinder: repo scan ─────────────────────────────────────────────

describe("ClassSourceFinder — repo scan", () => {
  let repoDir: string;

  beforeEach(async () => {
    repoDir = await tmpDir();
    // Mock execFile to succeed so decompilation works.
    vi.mocked(execFile).mockImplementation(((
      _cmd: unknown,
      _args: unknown,
      _opts: unknown,
      cb: unknown,
    ) => {
      if (typeof cb === "function") {
        (cb as (err: Error | null, stdout: string, stderr: string) => void)(
          null,
          'Compiled from "MyClass.java"\npublic class MyClass { public MyClass(); }\n',
          "",
        );
      }
    }) as any);
  });

  afterEach(() => {
    vi.mocked(execFile).mockImplementation(defaultExecFileImpl as any);
  });

  it("finds class in repo jar when project search fails", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "other.ts"), "not java");

    // Create a repo with a jar containing the class
    const jarPath = join(repoDir, "my-lib-1.0.jar");
    const zip = createZip([{ name: "com/example/Finder.class", data: Buffer.from("fake bytes") }]);
    writeFileSync(jarPath, zip);

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoDir],
    });
    const result = await finder.findSource("com.example.Finder");

    expect(result.found).toBe(true);
    if (result.found) {
      expect(result.method).toBe("m2-jar");
      expect(result.sourcePath).toContain("my-lib-1.0.jar");
      expect(result.source).toContain("public class MyClass");
    }
  });

  it("jarKeyword filters to matching jars only", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "main.ts"), "not java");

    // Both jars contain the same class name — only the "spring" one should be scanned
    const springJar = join(repoDir, "spring-core-6.1.0.jar");
    writeFileSync(
      springJar,
      createZip([{ name: "org/springframework/Bean.class", data: Buffer.from("fake") }]),
    );
    const otherJar = join(repoDir, "my-lib-1.0.jar");
    writeFileSync(
      otherJar,
      createZip([{ name: "org/springframework/Bean.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoDir],
    });
    const result = await finder.findSource("org.springframework.Bean", { jarKeyword: "spring" });

    expect(result.found).toBe(true);
    if (result.found) {
      // Should have found it in spring-core jar, not my-lib
      expect(result.sourcePath).toContain("spring-core");
    }
  });

  it("maxJarScan stops early when limit reached", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "other.ts"), "not java");

    // Create 5 jars. The target class is only in the 5th.
    for (let i = 1; i <= 5; i++) {
      const jp = join(repoDir, `lib-${i}.jar`);
      const entryName = i === 5 ? "com/example/Target.class" : "com/example/Other.class";
      writeFileSync(jp, createZip([{ name: entryName, data: Buffer.from("fake") }]));
    }

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoDir],
      maxJarScan: 3, // Only scan first 3 jars — Target is in jar 5
    });
    const result = await finder.findSource("com.example.Target");

    expect(result.found).toBe(false);
  });

  it("skips corrupt jar and continues scanning", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "other.ts"), "not java");

    // Corrupt jar (not valid zip)
    writeFileSync(join(repoDir, "corrupt.jar"), Buffer.from("not a zip file"));
    // Valid jar with the target class
    writeFileSync(
      join(repoDir, "valid.jar"),
      createZip([{ name: "com/example/Ok.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoDir],
    });
    const result = await finder.findSource("com.example.Ok");

    expect(result.found).toBe(true);
  });

  it("uses custom repoPaths when provided", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "other.ts"), "not java");

    // Two repo dirs, class in the second
    const repoA = await tmpDir();
    const repoB = await tmpDir();
    writeFileSync(
      join(repoB, "found.jar"),
      createZip([{ name: "com/example/Custom.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoA, repoB],
    });
    const result = await finder.findSource("com.example.Custom");

    expect(result.found).toBe(true);
  });
});

// ── ClassSourceFinder: decompilation chain ───────────────────────────────────

describe("ClassSourceFinder — decompilation", () => {
  beforeEach(() => {
    vi.mocked(execFile).mockImplementation(((
      _cmd: unknown,
      _args: unknown,
      _opts: unknown,
      cb: unknown,
    ) => {
      if (typeof cb === "function") {
        (cb as (err: Error | null, stdout: string, stderr: string) => void)(
          null,
          'Compiled from "StringUtils.java"\npublic class StringUtils {\n  public StringUtils();\n    Code:\n       0: aload_0\n       1: invokespecial #1\n       4: return\n  public static String upper(String);\n    Code:\n       0: aload_0\n       1: invokevirtual #2\n       4: areturn\n}\n',
          "",
        );
      }
    }) as any);
  });

  afterEach(() => {
    vi.mocked(execFile).mockImplementation(defaultExecFileImpl as any);
  });

  it("findSourceInJar reads + decompiles via javap", async () => {
    const root = await tmpDir();
    const jarPath = join(root, "commons-lang.jar");
    writeFileSync(
      jarPath,
      createZip([{ name: "org/apache/commons/StringUtils.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSourceInJar("org.apache.commons.StringUtils", jarPath);

    expect(result.found).toBe(true);
    if (result.found) {
      expect(result.method).toBe("jar");
      expect(result.sourcePath).toBe(jarPath);
      expect(result.source).toContain("public class StringUtils");
      expect(result.source).toContain("String upper(String)");
    }
  });

  it("findSource in repo mode returns javap output", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "main.ts"), "not java");

    const repoPath = await tmpDir();
    writeFileSync(
      join(repoPath, "guava-33.0.jar"),
      createZip([{ name: "com/google/common/base/Splitter.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoPath],
    });
    const result = await finder.findSource("com.google.common.base.Splitter");

    expect(result.found).toBe(true);
    if (result.found) {
      expect(result.method).toBe("m2-jar");
      expect(result.source).toContain("public class StringUtils");
    }
  });

  it("findSourceInJar returns not-found when entry missing", async () => {
    const root = await tmpDir();
    const jarPath = join(root, "partial.jar");
    writeFileSync(
      jarPath,
      createZip([{ name: "com/example/Present.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSourceInJar("com.example.Missing", jarPath);

    expect(result.found).toBe(false);
  });

  it("findSourceInJar returns not-found when javap fails", async () => {
    vi.mocked(execFile).mockImplementation(((
      _cmd: unknown,
      _args: unknown,
      _opts: unknown,
      cb: unknown,
    ) => {
      if (typeof cb === "function") {
        (cb as (err: Error | null, stdout: string, stderr: string) => void)(
          new Error("javap: command not found"),
          "",
          "javap: command not found",
        );
      }
    }) as any);

    const root = await tmpDir();
    const jarPath = join(root, "broken.jar");
    writeFileSync(
      jarPath,
      createZip([{ name: "com/example/Broken.class", data: Buffer.from("fake") }]),
    );

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSourceInJar("com.example.Broken", jarPath);

    // Should gracefully return not-found, not throw.
    expect(result.found).toBe(false);
  });
});

// ── ClassSourceFinder: AbortSignal mid-scan ──────────────────────────────────

describe("ClassSourceFinder — AbortSignal mid-scan", () => {
  it("can abort during jar directory walk", async () => {
    const projectRoot = await tmpDir();
    mkdirSync(join(projectRoot, "src"), { recursive: true });
    writeFileSync(join(projectRoot, "src", "other.ts"), "not java");

    // Seed a repo with many directories so the asynchronous walk is underway
    // when the abort fires.
    const repoDir = projectRoot;
    for (let i = 0; i < 20; i++) {
      const dir = join(repoDir, `repo-${i}`);
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, "lib.jar"), createZip([]));
    }

    const ctrl = new AbortController();
    const finder = new ClassSourceFinder({
      projectRoot,
      repoPaths: [repoDir],
      signal: ctrl.signal,
    });

    // Start the scan, then abort after the walk has begun.
    const promise = finder.findSource("com.example.X");
    // Yield the microtask queue so the walk enters its first readdir + loop.
    await new Promise((r) => setTimeout(r, 0));
    ctrl.abort();

    await expect(promise).rejects.toThrow("Aborted");
  });
});

// ── ClassSourceFinder: edge cases ────────────────────────────────────────────

describe("ClassSourceFinder — edge cases", () => {
  it("handles multiple .java files with same simple name — picks first encountered", async () => {
    const root = await tmpDir();
    // Two Java files with the same simple name in different packages
    const dirA = join(root, "src", "com", "foo");
    const dirB = join(root, "src", "org", "bar");
    mkdirSync(dirA, { recursive: true });
    mkdirSync(dirB, { recursive: true });
    writeFileSync(join(dirA, "Target.java"), "package com.foo;\npublic class Target {}");
    writeFileSync(join(dirB, "Target.java"), "package org.bar;\npublic class Target {}");

    const finder = new ClassSourceFinder({ projectRoot: root });
    // BFS order means com/foo found first
    const result = await finder.findSource("com.foo.Target");

    expect(result.found).toBe(true);
    if (result.found) {
      expect(result.source).toContain("com.foo");
    }
  });

  it("handles empty project root gracefully", async () => {
    const root = await tmpDir();
    const finder = new ClassSourceFinder({ projectRoot: root, repoPaths: [] });
    const result = await finder.findSource("com.example.Nonexistent");

    expect(result.found).toBe(false);
  }, 30_000);

  it("handles fully-qualified class name that is also a simple name", async () => {
    const root = await tmpDir();
    mkdirSync(root, { recursive: true });
    writeFileSync(join(root, "MyClass.java"), "public class MyClass {}");

    const finder = new ClassSourceFinder({ projectRoot: root });
    const result = await finder.findSource("MyClass");

    expect(result.found).toBe(true);
  });
});

// ── Tool registration tests ──────────────────────────────────────────────────

describe("registerJavaSourceTool", () => {
  it("registers tool with correct name, description, and schema", async () => {
    const reg = new ToolRegistry();
    registerJavaSourceTool(reg);

    const spec = reg.get("java_source");
    expect(spec).toBeDefined();
    expect(spec!.name).toBe("java_source");
    expect(spec!.description).toContain("Find and return Java source code");
    expect(spec!.readOnly).toBe(true);
    expect(spec!.parameters).toBeDefined();
    expect(spec!.parameters!.properties).toHaveProperty("className");
    expect(spec!.parameters!.required).toContain("className");
  });

  it("validates className is required — returns error JSON", async () => {
    const reg = new ToolRegistry();
    registerJavaSourceTool(reg);

    const result = await reg.dispatch("java_source", JSON.stringify({}));
    const parsed = JSON.parse(result);
    expect(parsed).toHaveProperty("error");
    expect(parsed.error).toContain("className");
  });

  it("validates empty className — returns error JSON", async () => {
    const reg = new ToolRegistry();
    registerJavaSourceTool(reg);

    const result = await reg.dispatch("java_source", JSON.stringify({ className: "  " }));
    const parsed = JSON.parse(result);
    expect(parsed).toHaveProperty("error");
    expect(parsed.error).toContain("className");
  });

  it("rejects invalid className format — returns error JSON", async () => {
    const reg = new ToolRegistry();
    registerJavaSourceTool(reg);

    const invalid = [
      "com.example.123class", // starts with digit
      "com..example.Foo", // double dot
      "com.example.Foo.Bar.", // trailing dot
      ".com.example.Foo", // leading dot
      "com/example/Foo", // slash instead of dot
      "com.example Foo", // space
    ];
    for (const cls of invalid) {
      const result = await reg.dispatch("java_source", JSON.stringify({ className: cls }));
      const parsed = JSON.parse(result);
      expect(parsed).toHaveProperty("error");
      expect(parsed.error).toContain("not a valid fully qualified Java class name");
    }
  });

  it("accepts valid class names — returns search result, not error", async () => {
    const root = await tmpDir();
    // Prevent slo-o-o-w repo scan by emptying defaultRepoPaths
    vi.spyOn(ClassSourceFinder, "defaultRepoPaths").mockReturnValue([]);
    const reg = new ToolRegistry();
    registerJavaSourceTool(reg, { projectRoot: root });

    const valid = [
      "com.example.MyClass",
      "MyClass",
      "java.lang.String",
      "com.example.Foo_Bar",
      "com.example.Foo$Bar",
      "a.b.C",
    ];
    for (const cls of valid) {
      const result = await reg.dispatch("java_source", JSON.stringify({ className: cls }));
      const parsed = JSON.parse(result);
      // Should be a search result (not-found), not a validation error
      expect(parsed).not.toHaveProperty("error");
      expect(parsed).toHaveProperty("status");
    }
  });

  it("mode 1: project search dispatch finds .java", async () => {
    const root = await tmpDir();
    const pkgDir = join(root, "src", "com", "test");
    mkdirSync(pkgDir, { recursive: true });
    writeFileSync(join(pkgDir, "Hello.java"), "package com.test;\npublic class Hello {}");

    const reg = new ToolRegistry();
    registerJavaSourceTool(reg, { projectRoot: root });

    const result = await reg.dispatch(
      "java_source",
      JSON.stringify({ className: "com.test.Hello" }),
    );
    const parsed = JSON.parse(result);
    expect(parsed.status).toBe("found");
    expect(parsed.method).toBe("project");
    expect(parsed.source).toContain("public class Hello");
    expect(parsed.sourcePath).toContain("Hello.java");
  });

  it("mode 2: jarPath dispatch reads + decompiles", async () => {
    const root = await tmpDir();
    const jarPath = join(root, "lib.jar");
    // Need execFile to return javap output
    vi.mocked(execFile).mockImplementation(((
      _cmd: unknown,
      _args: unknown,
      _opts: unknown,
      cb: unknown,
    ) => {
      if (typeof cb === "function") {
        (cb as (err: Error | null, stdout: string, stderr: string) => void)(
          null,
          'Compiled from "Util.java"\npublic class Util { public Util(); }\n',
          "",
        );
      }
    }) as any);
    writeFileSync(
      jarPath,
      createZip([{ name: "com/example/Util.class", data: Buffer.from("fake") }]),
    );

    const reg = new ToolRegistry();
    registerJavaSourceTool(reg, { projectRoot: root });

    const result = await reg.dispatch(
      "java_source",
      JSON.stringify({
        className: "com.example.Util",
        jarPath,
      }),
    );
    const parsed = JSON.parse(result);
    expect(parsed.status).toBe("found");
    expect(parsed.method).toBe("jar");
    expect(parsed.source).toContain("public class Util");
  });

  it("mode 3: jarKeyword dispatch accepts keyword without error", async () => {
    const root = await tmpDir();
    mkdirSync(join(root, "src"), { recursive: true });
    writeFileSync(join(root, "src", "main.ts"), "not java");
    // Prevent slo-o-o-w repo scan by emptying defaultRepoPaths
    vi.spyOn(ClassSourceFinder, "defaultRepoPaths").mockReturnValue([]);

    const reg = new ToolRegistry();
    registerJavaSourceTool(reg, { projectRoot: root });

    // The tool accepts jarKeyword and passes it to findSource.
    // Since repoPaths is empty, the result will be not-found.
    // We just verify the dispatch completes with a valid response.
    const result = await reg.dispatch(
      "java_source",
      JSON.stringify({
        className: "org.springframework.SomeClass",
        jarKeyword: "spring",
      }),
    );
    const parsed = JSON.parse(result);
    expect(parsed).toHaveProperty("status");
    expect(parsed.status).toBe("not-found");
    expect(parsed.className).toBe("org.springframework.SomeClass");
  });

  it("returns proper not-found response format", async () => {
    const root = await tmpDir();
    mkdirSync(join(root, "src"), { recursive: true });
    writeFileSync(join(root, "src", "main.ts"), "not java");
    // Prevent slo-o-o-w repo scan by emptying defaultRepoPaths
    vi.spyOn(ClassSourceFinder, "defaultRepoPaths").mockReturnValue([]);

    const reg = new ToolRegistry();
    registerJavaSourceTool(reg, { projectRoot: root });

    const result = await reg.dispatch(
      "java_source",
      JSON.stringify({ className: "com.example.Nonexistent" }),
    );
    const parsed = JSON.parse(result);
    expect(parsed.status).toBe("not-found");
    expect(parsed.className).toBe("com.example.Nonexistent");
    expect(parsed.message).toContain("No source found");
  });
});
