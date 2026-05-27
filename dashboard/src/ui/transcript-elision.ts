type TextSegmentLike = {
  kind: "text" | "reasoning";
  text: string;
};

type ToolSegmentLike = {
  kind: "tool";
  args: string;
  result?: string;
};

type SegmentLike = TextSegmentLike | ToolSegmentLike | { kind: string };

type AssistantMessageLike = {
  kind: "assistant";
  pending?: boolean;
  segments: SegmentLike[];
};

export const TRANSCRIPT_RECENT_MESSAGE_WINDOW = 120;
const MIN_ELIDE_CHARS = 4096;
const ELIDED_PREFIX = "[elided -- older than the last ";

function isAssistantMessage(value: unknown): value is AssistantMessageLike {
  return (
    typeof value === "object" &&
    value !== null &&
    (value as { kind?: unknown }).kind === "assistant" &&
    Array.isArray((value as { segments?: unknown }).segments)
  );
}

function isElided(value: unknown): boolean {
  return typeof value === "string" && value.startsWith(ELIDED_PREFIX);
}

function elidedStub(chars: number): string {
  return `${ELIDED_PREFIX}${TRANSCRIPT_RECENT_MESSAGE_WINDOW} messages; ${chars.toLocaleString()} chars dropped to keep long sessions responsive. Full content remains in the session log.]`;
}

function elideField(value: string): string {
  if (value.length <= MIN_ELIDE_CHARS || isElided(value)) return value;
  return elidedStub(value.length);
}

function elideSegment<S extends SegmentLike>(segment: S): S {
  if (
    (segment.kind === "text" || segment.kind === "reasoning") &&
    typeof (segment as TextSegmentLike).text === "string"
  ) {
    const textSegment = segment as TextSegmentLike;
    const text = elideField(textSegment.text);
    return text === textSegment.text ? segment : ({ ...segment, text } as S);
  }
  if (segment.kind !== "tool") return segment;

  const toolSegment = segment as ToolSegmentLike;
  let next: S | null = null;
  const args = elideField(toolSegment.args);
  if (args !== toolSegment.args) next = { ...segment, args } as S;
  if (typeof toolSegment.result === "string") {
    const result = elideField(toolSegment.result);
    if (result !== toolSegment.result) next = { ...(next ?? segment), result } as S;
  }
  return next ?? segment;
}

function elideSegments<S extends SegmentLike>(segments: S[]): S[] {
  let next: S[] | null = null;
  for (let i = 0; i < segments.length; i++) {
    const segment = segments[i];
    if (!segment) continue;
    const elided = elideSegment(segment);
    if (elided !== segment) {
      if (next === null) next = segments.slice();
      next[i] = elided;
    }
  }
  return next ?? segments;
}

export function elideTranscriptMessages<T>(messages: T[]): T[] {
  if (messages.length <= TRANSCRIPT_RECENT_MESSAGE_WINDOW) return messages;
  const cutoff = messages.length - TRANSCRIPT_RECENT_MESSAGE_WINDOW;
  let next: T[] | null = null;
  for (let i = 0; i < cutoff; i++) {
    const message = messages[i];
    if (!isAssistantMessage(message) || message.pending) continue;
    const segments = elideSegments(message.segments);
    if (segments !== message.segments) {
      if (next === null) next = messages.slice();
      next[i] = { ...message, segments } as T;
    }
  }
  return next ?? messages;
}
