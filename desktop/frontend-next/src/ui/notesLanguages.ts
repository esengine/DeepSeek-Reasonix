// A release's notes may carry a second language in the same document, after
// this line. The marker is invisible on the GitHub release page, where both
// languages read one after the other; the Versions panel shows one at a time.
export const NOTES_ENGLISH_MARK = "<!-- lang:en -->";

export interface NotesLanguages {
  first: string;
  english: string | null;
}

const rule = /\n+---\s*$/;

export function splitNotes(markdown: string): NotesLanguages {
  const at = markdown.indexOf(NOTES_ENGLISH_MARK);
  if (at < 0) return { first: markdown, english: null };
  const english = markdown.slice(at + NOTES_ENGLISH_MARK.length).trim();
  return { first: markdown.slice(0, at).replace(rule, "").trimEnd(), english: english || null };
}

// Language names are written in their own language and are not translated.
export const NOTES_LANGUAGE_NAMES = { first: "settings.text", english: "English" } as const;
