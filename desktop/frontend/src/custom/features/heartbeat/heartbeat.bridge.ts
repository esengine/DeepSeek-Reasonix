// Heartbeat panel bridge — typed wrappers around app heartbeat bindings.
// Custom components should import from here instead of calling app.* directly
// so that heartbeat-specific calls are scoped to this feature.

import { app } from "../../../lib/bridge";
import type { HeartbeatTask } from "./heartbeat.types";

interface HeartbeatConfigView {
  revision: number;
  etag: string;
  tasks: HeartbeatTask[];
}

let loadedConfigToken: Pick<HeartbeatConfigView, "revision" | "etag"> | null = null;

export function heartbeatListTasks(): Promise<HeartbeatTask[]> {
  return app.HeartbeatReloadConfig().then((raw) => {
    const view = (raw ?? { revision: 0, etag: "", tasks: [] }) as HeartbeatConfigView;
    loadedConfigToken = { revision: view.revision || 0, etag: view.etag || "" };
    return Array.isArray(view.tasks) ? view.tasks : [];
  });
}

export async function heartbeatSaveTasks(tasks: HeartbeatTask[]): Promise<void> {
  if (!loadedConfigToken) await heartbeatListTasks();
  const view = await app.HeartbeatSaveConfig({
    revision: loadedConfigToken?.revision || 0,
    etag: loadedConfigToken?.etag || "",
    tasks,
  });
  const saved = (view ?? { revision: 0, etag: "" }) as HeartbeatConfigView;
  loadedConfigToken = { revision: saved.revision || 0, etag: saved.etag || "" };
}

export function heartbeatTriggerNow(id: string): Promise<void> {
  return app.HeartbeatTriggerNow(id);
}

export function heartbeatGenerateID(): Promise<string> {
  return app.HeartbeatGenerateID();
}
