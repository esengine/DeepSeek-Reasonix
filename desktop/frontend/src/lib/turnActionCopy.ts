export function appendTurnActionCopyText(current: string, next: string): string {
  if (next.trim() === "") return current;
  if (current.trim() === "") return next;
  if (current.endsWith("\n") || next.startsWith("\n")) return current + next;
  return `${current}\n\n${next}`;
}
