// Heartbeat task types — mirrors desktop/heartbeat.go.

export interface HeartbeatTask {
  id: string;
  title: string;
  prompt: string;
  interval: string;   // e.g. "5m", "1h", "30s"
  enabled: boolean;
  scope?: string;      // "global" or "project"
  workspaceRoot?: string;
  topicId?: string;
  lastRunAt?: number;  // unix millis
  newConversationEachRun?: boolean; // true = create new topic each run
  createdAt?: number;
  approvalMode?: "ask" | "auto" | "yolo"; // empty defaults to "yolo"
  timeWindowStart?: string; // "HH:MM" — interval tasks only run after this time
  timeWindowEnd?: string;   // "HH:MM" — interval tasks only run before this time
  notifyChannels?: boolean; // true = push to bot channels; false/nil = skip
}
