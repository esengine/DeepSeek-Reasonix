import type { ProjectNode } from "./types";

export interface SessionCatalogStatus {
  state: "opening" | "ready" | "degraded" | "rebuilding" | "closed" | string;
  mode?: "disk" | "memory" | string;
  revision: number;
  indexed: number;
  total: number;
  repairPending: number;
  lastError?: string;
  quarantinedPath?: string;
}

export interface ProjectTreeSnapshot {
  revision: number;
  projects: ProjectNode[];
  catalog: SessionCatalogStatus;
  indexed: number;
  total: number;
  indexingDone: boolean;
}

export interface ProjectTopicPageRequest {
  scope: "global" | "project" | string;
  workspaceRoot?: string;
  cursor?: string;
  limit?: number;
  query?: string;
  timeFilter?: string;
  sortMode?: "created" | "updated" | string;
}

export interface ProjectTopicPage {
  items: ProjectNode[];
  nextCursor?: string;
  revision: number;
  complete?: boolean;
  readyDirectories?: number;
  pendingDirectories?: number;
  failedDirectories?: number;
}

export interface ProjectTopicKey {
  scope: "global" | "project" | string;
  workspaceRoot?: string;
  topicId: string;
}

export interface ProjectTreeChangedV2 {
  revision: number;
  roots: string[];
  reason: string;
}

export interface ProjectRuntimeTopic {
  scope: "global" | "project" | string;
  workspaceRoot?: string;
  node: ProjectNode;
}

export interface ProjectTreeRuntimeSnapshot {
  revision: number;
  topics: ProjectRuntimeTopic[];
}

export interface SessionCatalogBindings {
  GetProjectTreeSnapshot(): Promise<ProjectTreeSnapshot>;
  GetProjectTreeRuntimeSnapshot?(): Promise<ProjectTreeRuntimeSnapshot>;
  ListProjectTopics(req: ProjectTopicPageRequest): Promise<ProjectTopicPage>;
  GetTopicSummary(key: ProjectTopicKey): Promise<ProjectNode>;
  GetSessionCatalogStatus(): Promise<SessionCatalogStatus>;
  RebuildSessionCatalog(): Promise<void>;
}

// SessionReference is a session selected via @ past:chats for context injection.
export interface SessionReference {
  path: string;
  title: string;
  preview?: string;
  turns?: number;
  turnsState?: "unknown" | "valid" | "corrupt" | string;
  createdAt?: number;
  lastActivityAt?: number;
}
