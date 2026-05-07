/** CSI tail recovery for Ink useInput — Windows ConPTY splits `\x1b[A` across reads; we re-merge. */
/** Only rewrites when no structured key flag is set AND input matches a known tail exactly. */

/** Structured-flag subset of Ink's Key — optional across Ink versions. */
export interface CsiKeyFlags {
  upArrow?: boolean;
  downArrow?: boolean;
  leftArrow?: boolean;
  rightArrow?: boolean;
  pageUp?: boolean;
  pageDown?: boolean;
  delete?: boolean;
  shift?: boolean;
  tab?: boolean;
}

/** Bracketed-paste `[200~`/`[201~` excluded — handled by PromptInput's paste accumulator. */
const CSI_TAIL_TO_FLAGS: ReadonlyArray<{ tail: string; flags: CsiKeyFlags }> = [
  // Arrow keys — the most common ConPTY victim.
  { tail: "[A", flags: { upArrow: true } },
  { tail: "[B", flags: { downArrow: true } },
  { tail: "[C", flags: { rightArrow: true } },
  { tail: "[D", flags: { leftArrow: true } },
  // Page navigation.
  { tail: "[5~", flags: { pageUp: true } },
  { tail: "[6~", flags: { pageDown: true } },
  // Forward-delete (the key labelled Delete on most keyboards).
  { tail: "[3~", flags: { delete: true } },
  // Shift+Tab — terminal sends `\x1b[Z` rather than tab-with-shift.
  // `[1;2Z` is the modifier-encoded variant some Windows PowerShell
  // hosts emit; `[27;2;9~` and `[9;2u` cover modifyOtherKeys / Kitty
  // forms. Issue #373.
  { tail: "[Z", flags: { shift: true, tab: true } },
  { tail: "[1;2Z", flags: { shift: true, tab: true } },
  { tail: "[27;2;9~", flags: { shift: true, tab: true } },
  { tail: "[9;2u", flags: { shift: true, tab: true } },
];

function alreadyStructured(flags: CsiKeyFlags): boolean {
  return Boolean(
    flags.upArrow ||
      flags.downArrow ||
      flags.leftArrow ||
      flags.rightArrow ||
      flags.pageUp ||
      flags.pageDown ||
      flags.delete ||
      (flags.tab && flags.shift),
  );
}

/** Already-structured events short-circuit so a real arrow press isn't rewritten. */
export function recoverCsiTail(input: string, existing: CsiKeyFlags = {}): CsiKeyFlags | null {
  if (alreadyStructured(existing)) return null;
  for (const entry of CSI_TAIL_TO_FLAGS) {
    if (input === entry.tail || input === `\x1b${entry.tail}`) {
      return entry.flags;
    }
  }
  return null;
}

/** Includes paste `[200~`/`[201~` for the case where their markers chunked across reads. */
export const STRIPPABLE_CSI_FRAGMENTS: readonly string[] = [
  "\u001b[200~",
  "\u001b[201~",
  "[200~",
  "[201~",
  ...CSI_TAIL_TO_FLAGS.flatMap((e) => [`\u001b${e.tail}`, e.tail]),
];

/** Remove every recognised CSI fragment from a string. */
export function stripCsiFragments(input: string): string {
  let out = input;
  for (const frag of STRIPPABLE_CSI_FRAGMENTS) {
    if (out.includes(frag)) out = out.replaceAll(frag, "");
  }
  return out;
}
