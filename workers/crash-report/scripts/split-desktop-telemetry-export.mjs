import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, readdir, rm, stat, writeFile } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const ALLOWED_INSERTS = [
  'INSERT INTO "metrics" ',
  'INSERT INTO "pings" ',
];
const DATE_PATTERN = / VALUES\('(?<date>\d{4}-\d{2}-\d{2})'/;
const DATE_ONLY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

function parsePositiveInteger(value, name) {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isSafeInteger(parsed) || parsed < 1 || String(parsed) !== value) {
    throw new Error(`${name} must be a positive integer`);
  }
  return parsed;
}

export function parseArguments(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith("--") || value === undefined) {
      throw new Error("Arguments must be provided as --name value pairs");
    }
    values.set(key.slice(2), value);
  }

  const input = values.get("input");
  const outputDir = values.get("output-dir");
  const cutoff = values.get("cutoff");
  if (!input || !outputDir || !cutoff) {
    throw new Error("Required arguments: --input, --output-dir, --cutoff");
  }
  if (!DATE_ONLY_PATTERN.test(cutoff)) {
    throw new Error("--cutoff must use YYYY-MM-DD");
  }

  return {
    input: resolve(input),
    outputDir: resolve(outputDir),
    cutoff,
    maxStatements: parsePositiveInteger(values.get("max-statements") ?? "50000", "--max-statements"),
  };
}

async function closeStream(stream) {
  await new Promise((resolveClose, rejectClose) => {
    stream.once("error", rejectClose);
    stream.end(resolveClose);
  });
}

export async function splitDesktopTelemetryExport({ input, outputDir, cutoff, maxStatements }) {
  const inputInfo = await stat(input);
  if (!inputInfo.isFile()) throw new Error("Input must be a file");

  await mkdir(outputDir, { recursive: true });
  const existing = await readdir(outputDir);
  if (existing.length > 0) throw new Error("Output directory must be empty");

  const groups = {
    history: { files: [], statements: 0, bytes: 0, stream: null, chunkStatements: 0 },
    current: { files: [], statements: 0, bytes: 0, stream: null, chunkStatements: 0 },
  };

  async function startChunk(name) {
    const group = groups[name];
    const path = join(outputDir, `${name}-${String(group.files.length + 1).padStart(4, "0")}.sql`);
    group.stream = createWriteStream(path, { encoding: "utf8", flags: "wx" });
    group.chunkStatements = 0;
    group.files.push({ path, statements: 0, bytes: 0 });
  }

  async function append(name, sql) {
    const group = groups[name];
    if (!group.stream || group.chunkStatements >= maxStatements) {
      if (group.stream) await closeStream(group.stream);
      await startChunk(name);
    }
    const line = `${sql}\n`;
    if (!group.stream.write(line)) {
      await new Promise((resolveDrain) => group.stream.once("drain", resolveDrain));
    }
    const bytes = Buffer.byteLength(line);
    group.statements += 1;
    group.bytes += bytes;
    group.chunkStatements += 1;
    const file = group.files.at(-1);
    file.statements += 1;
    file.bytes += bytes;
  }

  let lineNumber = 0;
  try {
    const lines = createInterface({ input: createReadStream(input), crlfDelay: Infinity });
    for await (const rawLine of lines) {
      lineNumber += 1;
      const line = rawLine.trim();
      if (!line || line === "PRAGMA defer_foreign_keys=TRUE;") continue;
      if (!ALLOWED_INSERTS.some((prefix) => line.startsWith(prefix))) {
        throw new Error(`Unexpected SQL at line ${lineNumber}`);
      }
      const match = line.match(DATE_PATTERN);
      if (!match?.groups?.date || !line.endsWith(");")) {
        throw new Error(`Invalid telemetry INSERT at line ${lineNumber}`);
      }
      const name = match.groups.date < cutoff ? "history" : "current";
      await append(name, line.replace(/^INSERT INTO /, "INSERT OR REPLACE INTO "));
    }
    for (const group of Object.values(groups)) {
      if (group.stream) await closeStream(group.stream);
      group.stream = null;
    }
  } catch (error) {
    for (const group of Object.values(groups)) {
      group.stream?.destroy();
      group.stream = null;
    }
    await rm(outputDir, { recursive: true, force: true });
    throw error;
  }

  const manifest = {
    source: basename(input),
    sourceBytes: inputInfo.size,
    cutoff,
    maxStatements,
    history: { statements: groups.history.statements, bytes: groups.history.bytes, files: groups.history.files },
    current: { statements: groups.current.statements, bytes: groups.current.bytes, files: groups.current.files },
  };
  await writeFile(join(outputDir, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, { flag: "wx" });
  return manifest;
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  const manifest = await splitDesktopTelemetryExport(options);
  process.stdout.write(`${JSON.stringify(manifest, null, 2)}\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
