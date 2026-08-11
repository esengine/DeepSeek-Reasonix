import { replaceAttachmentRefsForDisplay } from "./attachmentDisplay";
import type { Item } from "./useController";

export type QuestionAnchor = { id: string; text: string; turn: number; checkpointTurn?: number };

export interface TurnGroup {
  userItem: Item;
  assistantPreview: string;
  toolCount: number;
  startIdx: number;
  endIdx: number;
}

export interface StepGroup {
  items: Item[];
  isFinal: boolean;
  isComplete: boolean;
}

export function questionAnchorId(id: string): string {
  return `question-anchor-${id}`;
}

export function compactQuestionText(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  if (cleaned.length <= 80) return cleaned;
  return cleaned.slice(0, 80);
}

export function questionTurnsById(questions: QuestionAnchor[]): Map<string, number> {
  const hasCheckpointTurns = questions.some((question) => question.checkpointTurn != null);
  const turns = new Map<string, number>();
  for (const question of questions) {
    if (question.checkpointTurn != null) {
      turns.set(question.id, question.checkpointTurn);
    } else if (!hasCheckpointTurns) {
      turns.set(question.id, question.turn);
    }
  }
  return turns;
}

export function lastQuestionTurn(questions: readonly QuestionAnchor[], turns: ReadonlyMap<string, number>): number | undefined {
  for (let i = questions.length - 1; i >= 0; i -= 1) {
    const turn = turns.get(questions[i].id);
    if (turn != null) return turn;
  }
  return undefined;
}

export function scrollVersion(items: Item[]): string {
  return items
    .map((it) => {
      switch (it.kind) {
        case "assistant":
          return `${it.id}:a:${it.streaming ? 1 : 0}`;
        case "tool":
          return `${it.id}:t:${it.status}`;
        default:
          return `${it.id}:${it.kind}`;
      }
    })
    .join("|");
}

function warmUserPreview(text: string): string {
  const cleaned = replaceAttachmentRefsForDisplay(text).replace(/\s+/g, " ").trim();
  return cleaned.length <= 80 ? cleaned : cleaned.slice(0, 77) + "...";
}

export function buildTurnGroups(items: Item[]): TurnGroup[] {
  const groups: TurnGroup[] = [];
  let start = -1;
  for (let i = 0; i < items.length; i += 1) {
    if (items[i].kind === "user") {
      if (start >= 0) {
        groups[groups.length - 1].endIdx = i;
      }
      start = i;
      groups.push({
        userItem: items[i],
        assistantPreview: "",
        toolCount: 0,
        startIdx: i,
        endIdx: items.length,
      });
    } else if (start >= 0 && groups.length > 0) {
      const group = groups[groups.length - 1];
      const item = items[i];
      if (item.kind === "assistant" && !item.streaming) {
        const previewText = item.text?.trim() || "";
        if (previewText) {
          group.assistantPreview = warmUserPreview(previewText);
        }
      }
      if (item.kind === "tool" && !item.parentId) {
        group.toolCount += 1;
      }
    }
  }
  return groups;
}

export function buildStepGroups(items: Item[], startIdx = 0): StepGroup[] {
  const groups: StepGroup[] = [];
  let current: Item[] = [];

  const flush = (isComplete: boolean) => {
    if (current.length === 0) return;
    groups.push({ items: current, isFinal: hasVisibleAssistantText(current), isComplete });
    current = [];
  };

  for (let i = startIdx; i < items.length; i++) {
    const it = items[i];
    if (it.kind === "user") {
      flush(true);
      groups.push({ items: [it], isFinal: false, isComplete: true });
      continue;
    }
    if (it.kind === "assistant") {
      flush(true);
    }
    current.push(it);
  }
  flush(false);
  return groups;
}

function hasVisibleAssistantText(items: Item[]): boolean {
  return items.some((it) => it.kind === "assistant" && !it.streaming && it.text.trim() !== "");
}
