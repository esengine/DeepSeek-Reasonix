// Heartbeat panel bridge — typed wrappers around app heartbeat bindings.
// Custom components should import from here instead of calling app.* directly
// so that heartbeat-specific calls are scoped to this feature.

import { app } from "../../../lib/bridge";
import type { HeartbeatTask } from "./heartbeat.types";

export function heartbeatListTasks(): Promise<HeartbeatTask[]> {
  return app.HeartbeatReloadTasks().then((v) => (v ?? []) as HeartbeatTask[]);
}

export function heartbeatSaveTasks(tasks: HeartbeatTask[]): Promise<void> {
  return app.HeartbeatSaveTasks(tasks as unknown);
}

export function heartbeatTriggerNow(id: string): Promise<void> {
  return app.HeartbeatTriggerNow(id);
}

export function heartbeatGenerateID(): Promise<string> {
  return app.HeartbeatGenerateID();
}
