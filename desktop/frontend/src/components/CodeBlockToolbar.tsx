import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Copy } from "lucide-react";
import { CopyButton } from "./CopyButton";
import { LANGS, languageLabel, rememberLang, suggestedLang } from "../lib/codeBlockActions";

// CodeBlockToolbar is the hover-revealed action row on a code block. It stacks
// four concerns in one row of <button>s:
//
//   1. Copy            — full plaintext (delegated to CopyButton).
//   2. Copy as Markdown — wraps the value in a fenced code block with the
//                          current language, so pasting into a GitHub issue
//                          preserves syntax highlighting. This is the action
//                          users reach for most when they want to share a
//                          snippet, and a separate plain "Copy" is the right
//                          escape hatch for things like piping into a shell.
//   3. Language picker — visible when no language was inferred (or always,
//                          to let the user override). The picker reads the
//                          remembered-most-recent for the active workspace
//                          and writes back on every change.
//   4. (Reserved) Run / Open in editor — wired through bridge.Todo once
//                          those bindings land; the toolbar reserves the
//                          slot so future addition is a 1-line change.
//
// The toolbar is mouse-driven for the picker but the buttons themselves are
// keyboard-reachable. Esc collapses an open picker without changing the
// selection. The hover-reveal is CSS-only (opacity transition on .code-block
// hover/focus-within) so we don't pay a re-render cost when the pointer
// crosses a code block boundary.
export function CodeBlockToolbar({
  value,
  language,
  workspace,
  onLanguageChange,
}: {
  value: string;
  language?: string;
  workspace?: string | null;
  onLanguageChange?: (next: string | null) => void;
}) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const [override, setOverride] = useState<string | null>(null);
  // effective language: an explicit override wins; otherwise the prop or the
  // suggested language for the workspace. Keeping it as a single string lets
  // the label render in one place.
  const effective = override ?? language ?? suggestedLang(workspace) ?? null;

  // Close the picker on outside click or Escape. The outside-click handler
  // is attached to document so it works across the picker popping over the
  // transcript; the Escape handler is local to the wrapper for clarity.
  useEffect(() => {
    if (!pickerOpen) return;
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current) return;
      if (!wrapRef.current.contains(e.target as Node)) setPickerOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setPickerOpen(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [pickerOpen]);

  const copyAsMarkdown = async () => {
    const fence = "```";
    const lang = effective ?? "";
    const body = `${fence}${lang}\n${value.replace(/\n$/, "")}\n${fence}\n`;
    try {
      await navigator.clipboard.writeText(body);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* clipboard unavailable */
    }
  };

  const pick = (id: string) => {
    setOverride(id);
    if (workspace) rememberLang(workspace, id);
    onLanguageChange?.(id);
    setPickerOpen(false);
  };

  return (
    <div className="code-block__toolbar" ref={wrapRef}>
      <CopyButton text={value} className="code-block__copy" />
      <button
        type="button"
        className="code-block__btn code-block__btn--md"
        onClick={copyAsMarkdown}
        title="Copy as Markdown"
        aria-label="Copy as Markdown"
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
        <span className="code-block__btn-label">MD</span>
      </button>
      <button
        type="button"
        className="code-block__btn code-block__lang"
        onClick={() => setPickerOpen((v) => !v)}
        title="Set language"
        aria-haspopup="listbox"
        aria-expanded={pickerOpen}
      >
        <span className="code-block__lang-label">{languageLabel(effective ?? undefined)}</span>
        <ChevronDown size={11} className={`code-block__lang-chev ${pickerOpen ? "code-block__lang-chev--open" : ""}`} />
      </button>
      {pickerOpen && (
        <ul className="code-block__picker" role="listbox" aria-label="Language">
          {LANGS.map((l) => (
            <li key={l.id}>
              <button
                type="button"
                role="option"
                aria-selected={effective === l.id}
                className={`code-block__picker-item ${effective === l.id ? "code-block__picker-item--on" : ""}`}
                onClick={() => pick(l.id)}
              >
                <span className="code-block__picker-label">{l.label}</span>
                <span className="code-block__picker-id">{l.id}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
