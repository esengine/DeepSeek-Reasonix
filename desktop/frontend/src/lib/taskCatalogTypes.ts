import type { TaskEvent, TaskSnapshot } from "./types";

export interface TaskPageRequest {
  scope: "session" | "project" | "all" | string; tabId: string; projectKey: string; states: string[]; query: string; cursor: string; limit: number;
}
export interface TaskCatalogStatus {
  state: string; mode: "disk" | "memory" | string; path?: string; revision: number;
  indexed: number; total: number; pending: number; failed: number; lastError?: string;
}
export interface TaskCatalogItem { projectKey: string; projectLabel: string; task: TaskSnapshot }
export interface TaskPage { items: TaskCatalogItem[]; nextCursor: string; revision: number; partial: boolean; staleCursor: boolean; status: TaskCatalogStatus }
export interface TaskEventPageRequest { projectKey: string; taskId: string; after: number; limit: number }
export interface TaskEventPage { items: TaskEvent[]; nextSequence: number; partial: boolean }
export interface TaskActionRequest { projectKey: string; taskId: string; expectedVersion: number; reason: string; idempotencyKey: string }
