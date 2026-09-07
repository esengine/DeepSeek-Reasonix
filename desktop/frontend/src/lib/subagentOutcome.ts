export type SubagentOutcome = readonly [
  ref: string | undefined,
  status: string | undefined,
  errorCode: string | undefined,
  retryable: boolean | undefined,
];

export function parseSubagentOutcomeText(text?: string): SubagentOutcome | undefined {
  if (!text) return undefined;
  const head = text.slice(0, 1024);
  const ref = head.match(/^Subagent reference(?: \(failed\))?: ([^\n]+)/m)?.[1]?.trim();
  const match = head.match(/^Subagent outcome: status=([^\s]+) retryable=(true|false)(?: error_code=([^\s]+))?/m);
  if (!ref || !match) return undefined;
  return [ref, match[1], match[3], match[2] === "true"];
}
