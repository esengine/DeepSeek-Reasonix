import { readFileSync } from "node:fs";

const path = process.env.INPUT_EMIT_FILE;
if (!path) {
  process.stderr.write("INPUT_EMIT_FILE not set\n");
  process.exit(2);
}

const lines = readFileSync(path, "utf8")
  .split("\n")
  .filter((l) => l.trim().length > 0);

for (const line of lines) {
  process.stdout.write(`${line}\n`);
}

process.stdout.end();
