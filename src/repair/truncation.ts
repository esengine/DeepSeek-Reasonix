/** Local-only repair (balance braces, close strings, fill nulls); continuation calls belong to the loop, which owns budgets. */

export interface TruncationRepairResult {
  repaired: string;
  changed: boolean;
  notes: string[];
  /** True when all repair attempts failed and the result falls back to "{}" — the original args are unrecoverable. */
  fallback: boolean;
}

export function repairTruncatedJson(input: string): TruncationRepairResult {
  const notes: string[] = [];
  if (!input || !input.trim()) {
    return {
      repaired: "{}",
      changed: input !== "{}",
      notes: ["empty input → {}"],
      fallback: false,
    };
  }
  // Fast path: already parseable.
  try {
    JSON.parse(input);
    return { repaired: input, changed: false, notes: [], fallback: false };
  } catch {
    /* fall through */
  }

  const stack: ("{" | "[" | '"')[] = [];
  let escaped = false;
  let inString = false;
  let lastSignificant = -1;

  for (let i = 0; i < input.length; i++) {
    const c = input[i]!;
    if (!/\s/.test(c)) lastSignificant = i;
    if (escaped) {
      escaped = false;
      continue;
    }
    if (inString) {
      if (c === "\\") {
        escaped = true;
        continue;
      }
      if (c === '"') {
        inString = false;
        stack.pop();
      }
      continue;
    }
    if (c === '"') {
      inString = true;
      stack.push('"');
      continue;
    }
    if (c === "{" || c === "[") stack.push(c);
    else if (c === "}" || c === "]") stack.pop();
  }

  let s = input.slice(0, lastSignificant + 1);

  // Trim a trailing comma which would block re-parse.
  if (/,$/.test(s)) {
    s = s.replace(/,$/, "");
    notes.push("trimmed trailing comma");
  }

  // If we ended on a key without a value: "foo": → "foo": null
  if (/"\s*:\s*$/.test(s)) {
    s += " null";
    notes.push("filled dangling key with null");
  }

  // If we ended inside a string, close it.
  if (inString) {
    s += '"';
    stack.pop();
    notes.push("closed unterminated string");
  }

  // Pop remaining open structures in reverse order.
  while (stack.length > 0) {
    const top = stack.pop();
    if (top === "{") s += "}";
    else if (top === "[") s += "]";
    else if (top === '"') s += '"';
  }

  try {
    JSON.parse(s);
    return { repaired: s, changed: s !== input, notes, fallback: false };
  } catch (err) {
    const preview =
      input.length <= 500 ? input : `${input.slice(0, 500)} …[+${input.length - 500} chars]`;
    notes.push(`fallback to {}: ${(err as Error).message}`);
    notes.push(`unrecoverable truncation — original args preview: ${preview}`);
    return { repaired: "{}", changed: true, notes, fallback: true };
  }
}
