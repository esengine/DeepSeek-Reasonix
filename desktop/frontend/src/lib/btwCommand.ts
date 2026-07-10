export function parseBtwCommandInput(text: string): string | null {
  const trimmed = text.trim();
  if (trimmed === "/btw") return "";
  const match = /^\/btw\s+([\s\S]+)$/.exec(trimmed);
  return match ? match[1].trim() : null;
}

export function isBtwCommand(text: string): boolean {
  return parseBtwCommandInput(text) !== null;
}
