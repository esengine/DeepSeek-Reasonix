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
  createdAt?: number;
}
