import type { ProjectNode } from "./types";

export interface SessionTitleBindings {
  AIRenameSession(topicID: string): Promise<string>;
}

export function mockAIRenameSession(topic?: ProjectNode | null): string {
  if (!topic) return "";
  const title = topic.preview?.trim() || topic.label?.replace(/^●\s*/, "").trim() || "";
  if (title) topic.label = `${topic.label?.startsWith("● ") ? "● " : ""}${title}`;
  return title;
}
