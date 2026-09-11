import type { ControlResult } from "./types";
import type {
  TaskActionRequest, TaskCatalogStatus, TaskEventPage, TaskEventPageRequest, TaskPage, TaskPageRequest,
} from "./taskCatalogTypes";

export interface TaskCatalogBindings {
  ListTaskPage(req: TaskPageRequest): Promise<TaskPage>;
  ListTaskEventPage(req: TaskEventPageRequest): Promise<TaskEventPage>;
  StopTaskByKey(req: TaskActionRequest): Promise<ControlResult>;
  CancelTaskByKey(req: TaskActionRequest): Promise<ControlResult>;
  RequeueTaskByKey(req: TaskActionRequest): Promise<ControlResult>;
  OpenTaskSessionByKey(req: { projectKey: string; taskId: string }): Promise<ControlResult>;
  GetTaskCatalogStatus(): Promise<TaskCatalogStatus>;
  RebuildTaskCatalog(): Promise<void>;
}

export function makeMockTaskCatalogBindings(): TaskCatalogBindings {
  const unavailable = (command: string): ControlResult => ({ schema_version: 1, command, task_id: "", accepted: false, idempotent: false, error: { code: "mock", message: "not available in browser mock" } });
  return {
    async ListTaskPage() { return { items: [], nextCursor: "", revision: 1, partial: false, staleCursor: false, status: await this.GetTaskCatalogStatus() }; },
    async ListTaskEventPage(req) { return { items: [], nextSequence: req.after, partial: false }; },
    async StopTaskByKey() { return unavailable("stop"); }, async CancelTaskByKey() { return unavailable("cancel"); },
    async RequeueTaskByKey() { return unavailable("requeue"); }, async OpenTaskSessionByKey() { return unavailable("open_session"); },
    async GetTaskCatalogStatus() { return { state: "ready", mode: "memory", revision: 1, indexed: 0, total: 0, pending: 0, failed: 0 }; },
    async RebuildTaskCatalog() {},
  };
}
