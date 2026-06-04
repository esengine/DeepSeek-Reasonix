// codeBlockActions centralizes the small utilities the code-block toolbar
// shares: the language-frequency list that powers the fallback picker, and
// the "remembered" map of (workspace, language) -> language that the picker
// uses for smart defaults.
//
// We keep this in lib/ rather than colocated with the toolbar so future
// components (e.g. a "set language" menu inside a future Monaco editor) can
// import the same constants without re-defining them.

// LANGS is the top-N set highlight.js can name, ordered roughly by frequency
// in real-world code blocks surfaced by the agent. The order is the picker's
// display order (most useful first). Anything not in this list still
// highlights — highlight.js autodetects — but the user can pick from this
// fixed menu to override.
export const LANGS: ReadonlyArray<{ id: string; label: string }> = [
  { id: "ts", label: "TypeScript" },
  { id: "tsx", label: "TSX" },
  { id: "js", label: "JavaScript" },
  { id: "jsx", label: "JSX" },
  { id: "go", label: "Go" },
  { id: "python", label: "Python" },
  { id: "py", label: "Python (alt)" },
  { id: "rust", label: "Rust" },
  { id: "java", label: "Java" },
  { id: "kotlin", label: "Kotlin" },
  { id: "swift", label: "Swift" },
  { id: "c", label: "C" },
  { id: "cpp", label: "C++" },
  { id: "csharp", label: "C#" },
  { id: "ruby", label: "Ruby" },
  { id: "php", label: "PHP" },
  { id: "bash", label: "Bash" },
  { id: "sh", label: "Shell" },
  { id: "sql", label: "SQL" },
  { id: "html", label: "HTML" },
  { id: "css", label: "CSS" },
  { id: "scss", label: "SCSS" },
  { id: "json", label: "JSON" },
  { id: "yaml", label: "YAML" },
  { id: "toml", label: "TOML" },
  { id: "xml", label: "XML" },
  { id: "markdown", label: "Markdown" },
  { id: "diff", label: "Diff" },
  { id: "dockerfile", label: "Dockerfile" },
  { id: "makefile", label: "Makefile" },
  { id: "plaintext", label: "Plain text" },
];

const LANG_LABEL: Record<string, string> = Object.fromEntries(LANGS.map((l) => [l.id, l.label]));

// languageLabel returns a human-friendly label for a stored language id, or
// the id itself when we don't recognize it (the picker's display then says
// "foo" rather than falling back to empty — better signal that a custom
// highlight is in use).
export function languageLabel(id: string | undefined): string {
  if (!id) return "auto";
  return LANG_LABEL[id] ?? id;
}

// rememberLang stores the user's last manual pick for a workspace. A "manual
// pick" is when the user changes the language in the picker — we use it as
// the default for the NEXT code block in the same workspace that arrives
// without a language, since the model usually gets it right but occasionally
// emits a bare `python` block the user wants to recolor.
const REMEMBER_KEY = "reasonix.codeblock.langs";

interface Remembered {
  // workspace id -> ordered list of recent picks, most-recent first, capped.
  byWorkspace: Record<string, string[]>;
}

function readRemembered(): Remembered {
  if (typeof localStorage === "undefined") return { byWorkspace: {} };
  try {
    const raw = localStorage.getItem(REMEMBER_KEY);
    if (!raw) return { byWorkspace: {} };
    const v = JSON.parse(raw);
    if (!v || typeof v !== "object" || !v.byWorkspace) return { byWorkspace: {} };
    return v as Remembered;
  } catch {
    return { byWorkspace: {} };
  }
}

function writeRemembered(v: Remembered): void {
  if (typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(REMEMBER_KEY, JSON.stringify(v));
  } catch {
    /* private mode — fine to forget */
  }
}

// rememberLang prepends `id` to the workspace's recent-pick list, dedupes,
// and caps at 5. The first entry of the returned list is `id`, so the
// caller's "next-default" logic is a single .byWorkspace[id]?.[0] lookup.
export function rememberLang(workspace: string, id: string): void {
  if (!workspace || !id) return;
  const v = readRemembered();
  const prev = v.byWorkspace[workspace] ?? [];
  const next = [id, ...prev.filter((x) => x !== id)].slice(0, 5);
  v.byWorkspace[workspace] = next;
  writeRemembered(v);
}

// suggestedLang returns the most-recent pick for `workspace` if the caller
// has no language. `null` means "no prior preference" — the picker should
// stay open without a default highlight.
export function suggestedLang(workspace: string | null | undefined): string | null {
  if (!workspace) return null;
  const v = readRemembered();
  return v.byWorkspace[workspace]?.[0] ?? null;
}
