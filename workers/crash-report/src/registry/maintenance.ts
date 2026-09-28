import { InstallRepo } from "./db/installs";
import { rescore } from "./db/ranking";
import { WINDOW_DAYS } from "./lib/ranking";

// Cron upkeep for the registry: scores decay by date with no write to trigger
// a recompute, and install ids older than the heat window count for nothing. A failure
// is logged and retried by the next run, never allowed to fail the others.
export async function maintainRegistry(db: D1Database, now: Date): Promise<void> {
  const today = now.toISOString().slice(0, 10);
  const oldest = new Date(now.getTime() - (WINDOW_DAYS - 1) * 86_400_000).toISOString().slice(0, 10);
  try {
    await rescore(db, today);
    await new InstallRepo(db).purgeBefore(oldest);
  } catch (err) {
    console.error("registry maintenance failed:", err);
  }
}
