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
let loadedTasks: HeartbeatTask[] = [];
let configQueue: Promise<void> = Promise.resolve();

function enqueueConfigOperation<T>(operation: () => Promise<T>): Promise<T> {
  const result = configQueue.then(operation, operation);
  configQueue = result.then(() => undefined, () => undefined);
  return result;
}

async function reloadConfig(): Promise<HeartbeatTask[]> {
  const raw = await app.HeartbeatReloadConfig();
  const view = (raw ?? { revision: 0, etag: "", tasks: [] }) as HeartbeatConfigView;
  loadedConfigToken = { revision: view.revision || 0, etag: view.etag || "" };
  loadedTasks = Array.isArray(view.tasks) ? view.tasks : [];
  return loadedTasks;
}

async function saveConfig(tasks: HeartbeatTask[]): Promise<HeartbeatTask[]> {
  const view = await app.HeartbeatSaveConfig({
    revision: loadedConfigToken?.revision || 0,
    etag: loadedConfigToken?.etag || "",
    tasks,
  });
  const saved = (view ?? { revision: 0, etag: "" }) as HeartbeatConfigView;
  loadedConfigToken = { revision: saved.revision || 0, etag: saved.etag || "" };
  loadedTasks = Array.isArray(saved.tasks) ? saved.tasks : tasks;
  return loadedTasks;
}

export function heartbeatListTasks(): Promise<HeartbeatTask[]> {
  return enqueueConfigOperation(reloadConfig);
}

export function heartbeatMutateTasks(mutate: (tasks: HeartbeatTask[]) => HeartbeatTask[]): Promise<HeartbeatTask[]> {
  return enqueueConfigOperation(async () => {
    if (!loadedConfigToken) await reloadConfig();
    const current = loadedTasks.map((task) => ({ ...task }));
    try {
      return await saveConfig(mutate(current));
    } catch (error) {
      try {
        await reloadConfig();
      } catch {
        // Preserve the original mutation error; the next operation will retry.
      }
      throw error;
    }
  });
}

export function heartbeatTriggerNow(id: string): Promise<void> {
  return app.HeartbeatTriggerNow(id);
}

export function heartbeatGenerateID(): Promise<string> {
  return app.HeartbeatGenerateID();
}
