import semver from "semver";
import { logError } from "./log.js";

/** Tolerant `>=` comparison for terminal-reported version strings. */
export function gte(a: string, b: string): boolean {
  try {
    return semver.gte(semver.coerce(a) ?? a, semver.coerce(b) ?? b);
  } catch (error) {
    logError(`semver gte(${a}, ${b}) failed: ${(error as Error).message}`);
    return false;
  }
}
