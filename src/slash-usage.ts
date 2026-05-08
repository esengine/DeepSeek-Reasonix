import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

export type SlashUsageCounts = Readonly<Record<string, number>>;

interface UsageFile {
  version: 1;
  counts: Record<string, number>;
}

export function slashUsagePath(): string {
  const override = process.env.REASONIX_SLASH_USAGE_PATH;
  if (override) return override;
  return join(homedir(), ".reasonix", "slash-usage.json");
}

export function loadSlashUsage(): SlashUsageCounts {
  const path = slashUsagePath();
  if (!existsSync(path)) return {};
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw) as Partial<UsageFile> | null;
    if (!parsed || typeof parsed !== "object") return {};
    const out: Record<string, number> = {};
    for (const [k, v] of Object.entries(parsed.counts ?? {})) {
      if (typeof v === "number" && Number.isFinite(v) && v >= 0) out[k] = v;
    }
    return out;
  } catch {
    return {};
  }
}

function persist(counts: Record<string, number>): void {
  const path = slashUsagePath();
  const tmp = `${path}.tmp`;
  const payload: UsageFile = { version: 1, counts };
  try {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(tmp, JSON.stringify(payload), "utf8");
    renameSync(tmp, path);
  } catch {
    /* disk full / perms — non-fatal, in-memory state still increments */
  }
}

/** Read-modify-write so two concurrent reasonix processes don't clobber each other's counts. */
export function recordSlashUse(name: string): SlashUsageCounts {
  const counts: Record<string, number> = { ...loadSlashUsage() };
  counts[name] = (counts[name] ?? 0) + 1;
  persist(counts);
  return counts;
}
