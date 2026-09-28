import { InstallRepo } from "./db/installs";
import { rescore } from "./db/ranking";

// Cron upkeep for the registry: scores decay by date with no write to trigger
// a recompute, and dedupe keys are dead once their day has passed. A failure
// is logged and retried by the next run, never allowed to fail the others.
export async function maintainRegistry(db: D1Database, now: Date): Promise<void> {
  const today = now.toISOString().slice(0, 10);
  const yesterday = new Date(now.getTime() - 86_400_000).toISOString().slice(0, 10);
  try {
    await rescore(db, today);
    await new InstallRepo(db).purgeBefore(yesterday);
  } catch (err) {
    console.error("registry maintenance failed:", err);
  }
}
