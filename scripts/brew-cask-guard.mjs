import { readFileSync, realpathSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { compareSemver } from "../npm/publish.mjs";

const VERSION_LINE = /^\s*version "([^"]+)"\s*$/m;

// Returns "write" when the candidate is not older than the tap's cask, "skip"
// when the tap is ahead; throws when the tap's version cannot be read.
export function caskDecision(caskText, candidate) {
  const current = caskText.match(VERSION_LINE)?.[1];
  if (!current) throw new Error("the tap's cask has no readable version line; refusing to overwrite it");
  return compareSemver(candidate, current) < 0 ? "skip" : "write";
}

if (process.argv[1] && import.meta.url === pathToFileURL(realpathSync(process.argv[1])).href) {
  const [file, candidate] = process.argv.slice(2);
  if (!file || !candidate) throw new Error("usage: brew-cask-guard.mjs CASK_FILE VERSION");
  try {
    console.log(caskDecision(readFileSync(file, "utf8"), candidate));
  } catch (error) {
    console.error(`::error::${error.message}`);
    process.exit(1);
  }
}
