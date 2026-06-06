// Java source resolver: project tree → ~/.m2 + ~/.gradle jar cache → javap decompile.

import { execFile } from "node:child_process";
import * as fs from "node:fs";
import * as fsp from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";
import { readJarEntry } from "./zip-reader.js";

export interface FindResultSuccess {
  found: true;
  source: string;
  method: "project" | "m2-source-jar" | "m2-jar" | "jar";
  sourcePath: string;
}

export interface FindResultNotFound {
  found: false;
  method: "not-found";
}

export type FindResult = FindResultSuccess | FindResultNotFound;

export interface FindSourceOptions {
  /** Case-insensitive substring match against jar path; dramatically narrows the cache scan. */
  jarKeyword?: string;
}

export interface ClassSourceFinderOptions {
  projectRoot: string;
  /** Jar cache dirs. When absent, auto-detects ~/.m2/repository + ~/.gradle/caches. */
  repoPaths?: string[];
  javapCommand?: string;
  /** Cap on jars walked before bailing. */
  maxJarScan?: number;
  signal?: AbortSignal;
}

export class ClassSourceFinder {
  private projectRoot: string;
  private repoPaths: string[];
  private javapCommand: string;
  private maxJarScan: number;
  private signal?: AbortSignal;

  static defaultRepoPaths(): string[] {
    const home = os.homedir();
    const candidates = [path.join(home, ".m2", "repository"), path.join(home, ".gradle", "caches")];
    return candidates.filter((p) => fs.existsSync(p));
  }

  constructor(options: ClassSourceFinderOptions) {
    this.projectRoot = path.resolve(options.projectRoot);
    this.repoPaths =
      options.repoPaths && options.repoPaths.length > 0
        ? options.repoPaths.map((p) => path.resolve(p))
        : ClassSourceFinder.defaultRepoPaths();
    this.javapCommand = options.javapCommand ?? "javap";
    this.maxJarScan = options.maxJarScan ?? 2000;
    this.signal = options.signal;
  }

  async findSource(fullyQualifiedName: string, options?: FindSourceOptions): Promise<FindResult> {
    this.throwIfAborted();
    const projectResult = await this.searchProject(fullyQualifiedName);
    if (projectResult) return projectResult;
    return this.searchRepositories(fullyQualifiedName, options?.jarKeyword);
  }

  async findSourceInJar(fullyQualifiedName: string, jarPath: string): Promise<FindResult> {
    this.throwIfAborted();

    const resolvedJarPath = path.resolve(jarPath);
    if (!fs.existsSync(resolvedJarPath)) {
      return { found: false, method: "not-found" };
    }

    const classEntry = `${fullyQualifiedName.replace(/\./g, "/")}.class`;
    try {
      const content = readJarEntry(resolvedJarPath, classEntry);
      if (!content) return { found: false, method: "not-found" };
      const source = await this.decompileFromJar(resolvedJarPath, content.data, fullyQualifiedName);
      return { found: true, source, method: "jar", sourcePath: resolvedJarPath };
    } catch (err) {
      return { found: false, method: "not-found" };
    }
  }

  private async searchProject(fqn: string): Promise<FindResultSuccess | null> {
    const simpleName = this.simpleClassName(fqn);
    const suffixes = [`${simpleName}.java`, `${simpleName}.java.txt`];

    const queue: string[] = [this.projectRoot];
    while (queue.length > 0) {
      this.throwIfAborted();
      const dir = queue.shift()!;
      let entries: fs.Dirent[];
      try {
        entries = await fsp.readdir(dir, { withFileTypes: true });
      } catch {
        continue;
      }

      for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);

        if (entry.isDirectory()) {
          if (
            entry.name === "node_modules" ||
            entry.name === ".git" ||
            entry.name === "target" ||
            entry.name === "build" ||
            entry.name === "dist" ||
            entry.name === ".idea" ||
            entry.name === ".vscode" ||
            entry.name === ".gradle"
          ) {
            continue;
          }
          queue.push(fullPath);
        } else if (entry.isFile()) {
          if (suffixes.includes(entry.name)) {
            const source = await fsp.readFile(fullPath, "utf-8");
            return { found: true, source, method: "project", sourcePath: fullPath };
          }
        }
      }
    }

    return null;
  }

  private async searchRepositories(fqn: string, jarKeyword?: string): Promise<FindResult> {
    const javaEntry = `${fqn.replace(/\./g, "/")}.java`;
    const classEntry = `${fqn.replace(/\./g, "/")}.class`;

    const sourceJars: string[] = [];
    const regularJars: string[] = [];
    for (const repoDir of this.repoPaths) {
      await this.walkForJars(repoDir, sourceJars, regularJars, jarKeyword);
    }

    // Pass 1: prefer source jars — read .java directly, no decompile.
    for (const jarPath of sourceJars) {
      this.throwIfAborted();
      try {
        const content = readJarEntry(jarPath, javaEntry);
        if (content) {
          return {
            found: true,
            source: content.data.toString("utf-8"),
            method: "m2-source-jar",
            sourcePath: jarPath,
          };
        }
      } catch {
        // skip
      }
    }

    // Pass 2: fall back to regular jars — decompile .class via javap.
    let scanned = 0;
    for (const jarPath of regularJars) {
      this.throwIfAborted();
      if (scanned >= this.maxJarScan) break;

      scanned++;
      try {
        const content = readJarEntry(jarPath, classEntry);
        if (content) {
          const source = await this.decompileFromJar(jarPath, content.data, fqn);
          return { found: true, source, method: "m2-jar", sourcePath: jarPath };
        }
      } catch {
        // skip
      }
    }

    return { found: false, method: "not-found" };
  }

  private static readonly MAX_WALK_DEPTH = 64;

  private async walkForJars(
    dir: string,
    sourceJars: string[],
    regularJars: string[],
    keyword?: string,
    depth = 0,
  ): Promise<void> {
    if (depth >= ClassSourceFinder.MAX_WALK_DEPTH) return;

    let entries: fs.Dirent[];
    try {
      entries = await fsp.readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }

    for (const entry of entries) {
      this.throwIfAborted();
      if (sourceJars.length + regularJars.length >= this.maxJarScan) return;

      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        await this.walkForJars(fullPath, sourceJars, regularJars, keyword, depth + 1);
      } else if (entry.isFile() && entry.name.endsWith(".jar")) {
        if (!keyword || fullPath.toLowerCase().includes(keyword.toLowerCase())) {
          if (entry.name.endsWith("-sources.jar") || entry.name.includes("-sources-")) {
            sourceJars.push(fullPath);
          } else {
            regularJars.push(fullPath);
          }
        }
      }
    }
  }

  private async decompileFromJar(
    jarPath: string,
    classBytes: Buffer,
    fqn: string,
  ): Promise<string> {
    const tmpDir = await fsp.mkdtemp(path.join(os.tmpdir(), "reasonix-java-src-"));

    try {
      const pkgPath = fqn.replace(/\./g, path.sep);
      const classDir = path.join(tmpDir, path.dirname(pkgPath));
      await fsp.mkdir(classDir, { recursive: true });

      const classFile = path.join(tmpDir, `${pkgPath}.class`);
      await fsp.writeFile(classFile, classBytes);

      const raw = await this.runJavap(fqn, tmpDir);
      return ClassSourceFinder.compressJavapOutput(raw);
    } finally {
      await fsp.rm(tmpDir, { recursive: true, force: true }).catch(() => {});
    }
  }

  // Strip constant pool, debug tables (LineNumberTable / LocalVariableTable /
  // StackMapTable), and "Compiled from …" from javap output — cuts ~60-80% of
  // tokens for AI consumption while keeping method sigs + bytecode.
  private static compressJavapOutput(raw: string): string {
    const lines = raw.split("\n");
    const out: string[] = [];
    let skipUntilIndent: number | null = null;

    for (const line of lines) {
      // Constant pool entry lines — always indented `#N = ...`
      if (/^\s+#\d+\s*=/.test(line)) continue;

      // Inside a debug table body — skip lines indented deeper than the header
      if (skipUntilIndent !== null) {
        const m = line.match(/^(\s*)/);
        const indent = m?.[1]?.length ?? 0;
        if (indent > skipUntilIndent) continue;
        skipUntilIndent = null;
        // Also eat the blank line that often separates the table from the next section
        if (line.trim() === "") continue;
      }

      // Debug table header — note its indent so we know when the body ends
      const debugMatch = line.match(/^(\s+)(LineNumberTable|LocalVariableTable|StackMapTable):/);
      if (debugMatch) {
        skipUntilIndent = debugMatch[1]!.length;
        continue;
      }

      // "Compiled from …" preamble
      if (line.startsWith("Compiled from ")) continue;

      out.push(line);
    }

    return out
      .join("\n")
      .replace(/\n{3,}/g, "\n\n") // collapse multiple blank lines
      .trim();
  }

  private runJavap(className: string, classPath: string): Promise<string> {
    return new Promise((resolve, reject) => {
      execFile(
        this.javapCommand,
        ["-c", "-p", "-cp", classPath, className],
        {
          maxBuffer: 15 * 1024 * 1024,
          timeout: 30_000,
          signal: this.signal,
        },
        (err, stdout, stderr) => {
          if (err) {
            // javap exits non-zero on missing class / unsupported bytecode — keep its diagnostics.
            const msg = [stdout, stderr].filter(Boolean).join("\n") || err.message;
            reject(new Error(`javap failed: ${msg}`));
            return;
          }
          resolve(stdout);
        },
      );
    });
  }

  private simpleClassName(fqn: string): string {
    const lastDot = fqn.lastIndexOf(".");
    return lastDot === -1 ? fqn : fqn.slice(lastDot + 1);
  }

  private throwIfAborted(): void {
    if (this.signal?.aborted) {
      throw new DOMException("Aborted", "AbortError");
    }
  }
}
