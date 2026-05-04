/** Submit-history walker for SimplePromptInput. ↑ recalls older entries, ↓ walks forward. */

import { useRef } from "react";

export interface PromptHistory {
  /** Push the just-submitted text. Empty / duplicate-of-latest entries are ignored. */
  recordSubmit(text: string): void;
  /** Step back one entry. Saves currentValue as the restored draft on the
   *  very first prev call so ↓-past-newest can restore it. Returns the
   *  recalled string or null when there's nothing older. */
  recallPrev(currentValue: string): string | null;
  /** Step forward one entry. Returns the recalled string, the saved draft
   *  when stepping past the newest entry, or null when not currently
   *  recalling anything. */
  recallNext(): string | null;
  /** Drop the recall cursor + saved draft (e.g. on a new submit). */
  reset(): void;
}

export function usePromptHistory(maxEntries = 100): PromptHistory {
  // Refs throughout — recall walking shouldn't trigger re-renders. The
  // parent's `onChange` from SimplePromptInput is what propagates the
  // recalled value back into the controlled input.
  const entriesRef = useRef<string[]>([]);
  const indexRef = useRef<number | null>(null);
  const draftRef = useRef<string>("");

  return {
    recordSubmit(text) {
      if (text.length === 0) return;
      const entries = entriesRef.current;
      if (entries[entries.length - 1] === text) {
        indexRef.current = null;
        draftRef.current = "";
        return;
      }
      entries.push(text);
      if (entries.length > maxEntries) entries.splice(0, entries.length - maxEntries);
      indexRef.current = null;
      draftRef.current = "";
    },
    recallPrev(currentValue) {
      const entries = entriesRef.current;
      if (entries.length === 0) return null;
      if (indexRef.current === null) {
        draftRef.current = currentValue;
        indexRef.current = entries.length - 1;
        return entries[indexRef.current] ?? null;
      }
      if (indexRef.current === 0) return null;
      indexRef.current -= 1;
      return entries[indexRef.current] ?? null;
    },
    recallNext() {
      const entries = entriesRef.current;
      if (indexRef.current === null) return null;
      if (indexRef.current >= entries.length - 1) {
        // Past the newest → restore the saved draft and exit recall mode.
        const draft = draftRef.current;
        indexRef.current = null;
        draftRef.current = "";
        return draft;
      }
      indexRef.current += 1;
      return entries[indexRef.current] ?? null;
    },
    reset() {
      indexRef.current = null;
      draftRef.current = "";
    },
  };
}
