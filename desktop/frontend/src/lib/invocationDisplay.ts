import type { CommandInfo } from "./types";

export type InvocationKind = "skill" | "subagent";
export type InvocationMetadata = { kind: InvocationKind; color?: string };
export type InvocationMetadataMap = Readonly<Record<string, InvocationMetadata>>;

export type InvocationDisplay = {
  name: string;
  label: string;
  source?: string;
  kind?: InvocationKind;
  color?: string;
};

export type ComposerInvocation = {
  id: string;
  offset: number;
  command: CommandInfo;
};

export type InvocationRequest = {
  name: string;
  kind: InvocationKind;
  offset: number;
};

export type StructuredInvocationSubmit = {
  display: string;
  input: string;
  invocations: InvocationRequest[];
};

export function invocationRequests(invocations: ComposerInvocation[]): InvocationRequest[] {
  return sortComposerInvocations(invocations).map((invocation) => ({
    name: invocation.command.name,
    kind: invocation.command.kind === "subagent" ? "subagent" : "skill",
    offset: invocation.offset,
  }));
}

export type InvocationTextSegment =
  | { type: "text"; content: string; start: number }
  | { type: "invocation"; invocation: InvocationDisplay; offset: number };

const invocationNamePattern = "[A-Za-z0-9_.:-]+";
const knownSubagents = new Set(["general-purpose", "explore", "research", "review", "security_review"]);

export function commandUsesStructuredInvocation(command: CommandInfo): boolean {
  return command.kind === "skill" || command.kind === "subagent";
}

export function invocationLabel(name: string): string {
  const unqualified = name.split(":").pop() || name;
  return unqualified
    .split(/[-_.]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function invocationDisplayForCommand(command: CommandInfo): InvocationDisplay {
  return {
    name: command.name,
    label: invocationLabel(command.name),
    source: command.plugin || command.name.split(":").slice(0, -1).join(":") || undefined,
    kind: command.kind === "subagent" ? "subagent" : "skill",
    color: command.color,
  };
}

export function sortComposerInvocations(invocations: ComposerInvocation[]): ComposerInvocation[] {
  return invocations
    .map((invocation, index) => ({ invocation, index }))
    .sort((a, b) => a.invocation.offset - b.invocation.offset || a.index - b.index)
    .map(({ invocation }) => invocation);
}

export function replaceInvocationTextRange(
  text: string,
  invocations: ComposerInvocation[],
  start: number,
  end: number,
  value: string,
  afterInvocationId?: string,
): { text: string; invocations: ComposerInvocation[] } {
  const from = Math.max(0, Math.min(start, end, text.length));
  const to = Math.max(from, Math.min(Math.max(start, end), text.length));
  const delta = value.length - (to - from);
  const ordered = sortComposerInvocations(invocations);
  const anchorIndex = from === to && afterInvocationId
    ? ordered.findIndex((invocation) => invocation.id === afterInvocationId && invocation.offset === from)
    : -1;
  const nextInvocations = ordered
    .filter((invocation) => invocation.offset <= from || invocation.offset >= to)
    .map((invocation, index) => {
      const shiftAtInsertion = from === to
        && invocation.offset === to
        && (anchorIndex < 0 || index > anchorIndex);
      const shiftAfterRange = invocation.offset > to || (from < to && invocation.offset === to);
      return {
        ...invocation,
        offset: shiftAtInsertion || shiftAfterRange ? invocation.offset + delta : invocation.offset,
      };
    });
  return {
    text: text.slice(0, from) + value + text.slice(to),
    invocations: sortComposerInvocations(nextInvocations),
  };
}

export function trimInvocationDraft(
  text: string,
  invocations: ComposerInvocation[],
): { text: string; invocations: ComposerInvocation[] } {
  const trimmedStart = text.trimStart();
  const leading = text.length - trimmedStart.length;
  const trimmed = trimmedStart.trimEnd();
  return {
    text: trimmed,
    invocations: sortComposerInvocations(invocations.map((invocation) => ({
      ...invocation,
      offset: Math.max(0, Math.min(trimmed.length, invocation.offset - leading)),
    }))),
  };
}

export function serializeInvocationSubmit(text: string, invocations: ComposerInvocation[]): string {
  const ordered = sortComposerInvocations(invocations);
  if (ordered.length === 0) return text;

  let cursor = 0;
  let output = "";
  for (const invocation of ordered) {
    const offset = Math.max(cursor, Math.min(text.length, invocation.offset));
    output += text.slice(cursor, offset);
    if (output && !/\s$/.test(output)) output += " ";
    output += `/${invocation.command.name}`;
    if (offset < text.length && !/^\s/.test(text.slice(offset))) output += " ";
    cursor = offset;
  }
  output += text.slice(cursor);
  return output;
}

function invocationBody(submitText: string): string {
  const sessionQuestionMarker = "当前用户问题：\n";
  const markerIndex = submitText.lastIndexOf(sessionQuestionMarker);
  return (markerIndex >= 0 ? submitText.slice(markerIndex + sessionQuestionMarker.length) : submitText).trim();
}

type SlashMatch = { start: number; end: number; name: string };

function slashMatches(text: string): SlashMatch[] {
  const re = new RegExp(`/${invocationNamePattern}(?=\\s|$)`, "g");
  return Array.from(text.matchAll(re), (match) => ({
    start: match.index ?? 0,
    end: (match.index ?? 0) + match[0].length,
    name: match[0].slice(1),
  }));
}

function chunkVariants(chunk: string): string[] {
  const values = [chunk];
  if (chunk.startsWith(" ")) values.push(chunk.slice(1));
  if (chunk.endsWith(" ")) values.push(chunk.slice(0, -1));
  if (chunk.startsWith(" ") && chunk.endsWith(" ") && chunk.length > 1) values.push(chunk.slice(1, -1));
  return Array.from(new Set(values));
}

function segmentsForSelection(
  submit: string,
  display: string,
  matches: SlashMatch[],
  mask: number,
  invocationMetadata: InvocationMetadataMap,
): InvocationTextSegment[] | null {
  const selected = matches.filter((_, index) => (mask & (1 << index)) !== 0);
  if (selected.length === 0) return null;

  const normalizedDisplay = display.trim();
  const chunks: string[] = [];
  let cursor = 0;
  selected.forEach((match) => {
    chunks.push(submit.slice(cursor, match.start));
    cursor = match.end;
  });
  chunks.push(submit.slice(cursor));

  let resolved: { text: string; offsets: number[] } | null = null;
  const resolve = (index: number, text: string, offsets: number[]) => {
    if (resolved || !normalizedDisplay.startsWith(text)) return;
    if (index === chunks.length) {
      if (text === normalizedDisplay) resolved = { text, offsets };
      return;
    }
    for (const variant of chunkVariants(chunks[index])) {
      const nextText = text + variant;
      if (!normalizedDisplay.startsWith(nextText)) continue;
      const nextOffsets = index < selected.length ? [...offsets, nextText.length] : offsets;
      resolve(index + 1, nextText, nextOffsets);
    }
  };
  resolve(0, "", []);
  if (!resolved) return null;

  const segments: InvocationTextSegment[] = [];
  let textCursor = 0;
  selected.forEach((match, index) => {
    const offset = resolved!.offsets[index];
    if (offset > textCursor) {
      segments.push({ type: "text", content: normalizedDisplay.slice(textCursor, offset), start: textCursor });
    }
    segments.push({
      type: "invocation",
      offset,
      invocation: {
        name: match.name,
        label: invocationLabel(match.name),
        source: match.name.includes(":") ? match.name.split(":").slice(0, -1).join(":") : undefined,
        kind: invocationMetadata[match.name]?.kind ?? (knownSubagents.has(match.name) ? "subagent" : "skill"),
        color: invocationMetadata[match.name]?.color,
      },
    });
    textCursor = offset;
  });
  if (textCursor < normalizedDisplay.length) {
    segments.push({ type: "text", content: normalizedDisplay.slice(textCursor), start: textCursor });
  }
  return segments;
}

// hydratedSlashFallbackSegments restores badges for a hydrated structured
// message: session reload resolves the recorded display — the serialized
// slash form ("/name task") — while the submit side is the composed model
// text with no slash tokens, so the display/submit pairing above never
// matches. Only the dominant serialized shape is restored: consecutive
// known-command tokens at the very start followed by the task text. Prose
// slashes, unknown names, and mid-text entities all bail to plain text.
function hydratedSlashFallbackSegments(
  display: string,
  invocationMetadata: InvocationMetadataMap,
): InvocationTextSegment[] | null {
  const matches = slashMatches(display);
  if (matches.length === 0 || matches.length > 10) return null;
  const leading: SlashMatch[] = [];
  let cursor = 0;
  for (const match of matches) {
    if (match.start !== cursor || !invocationMetadata[match.name]) break;
    leading.push(match);
    cursor = display[match.end] === " " ? match.end + 1 : match.end;
  }
  if (leading.length === 0 || leading.length !== matches.length) return null;
  const segments: InvocationTextSegment[] = leading.map((match) => ({
    type: "invocation" as const,
    offset: 0,
    invocation: {
      name: match.name,
      label: invocationLabel(match.name),
      source: match.name.includes(":") ? match.name.split(":").slice(0, -1).join(":") : undefined,
      kind: invocationMetadata[match.name]?.kind ?? (knownSubagents.has(match.name) ? "subagent" : "skill"),
      color: invocationMetadata[match.name]?.color,
    },
  }));
  const remainder = display.slice(cursor);
  if (remainder) segments.push({ type: "text", content: remainder, start: 0 });
  return segments;
}

export function invocationSegmentsFromMessage(
  displayText: string,
  submitText?: string,
  invocationMetadata: InvocationMetadataMap = {},
): InvocationTextSegment[] {
  const display = displayText.trim();
  const submit = invocationBody(submitText?.trim() ?? "");
  if (!submit || submit === display) return [{ type: "text", content: display, start: 0 }];

  const matches = slashMatches(submit);
  if (matches.length > 0 && matches.length <= 10) {
    const masks = 1 << matches.length;
    for (let mask = masks - 1; mask > 0; mask -= 1) {
      const segments = segmentsForSelection(submit, display, matches, mask, invocationMetadata);
      if (segments) return segments;
    }
  }
  return hydratedSlashFallbackSegments(display, invocationMetadata) ?? [{ type: "text", content: display, start: 0 }];
}

export function invocationDisplayFromMessage(displayText: string, submitText?: string): InvocationDisplay | null {
  const segment = invocationSegmentsFromMessage(displayText, submitText).find((item) => item.type === "invocation");
  return segment?.type === "invocation" ? segment.invocation : null;
}
