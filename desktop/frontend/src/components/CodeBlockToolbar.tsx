import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { CopyButton } from "./CopyButton";

export function buildMarkdownCodeBlock(value: string, language?: string): string {
  let longestBacktickRun = 0;
  for (const match of value.matchAll(/`+/g)) {
    longestBacktickRun = Math.max(longestBacktickRun, match[0].length);
  }
  const fence = "`".repeat(Math.max(3, longestBacktickRun + 1));
  const lang = language?.trim() ?? "";
  return `${fence}${lang}\n${value.replace(/\n$/, "")}\n${fence}\n`;
}

// CodeBlockToolbar keeps code-block actions outside the scrollable <pre>, so
// copy controls stay visible for long snippets while HljsCode remains a pure
// syntax-highlighting renderer.
export function CodeBlockToolbar({
  value,
  language,
}: {
  value: string;
  language?: string;
}) {
  const [copied, setCopied] = useState(false);
  const label = language?.trim() || "auto";

  const copyAsMarkdown = async () => {
    try {
      await navigator.clipboard.writeText(buildMarkdownCodeBlock(value, language));
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      /* clipboard unavailable */
    }
  };

  return (
    <div className="code-block__toolbar" aria-label="Code block actions">
      <span className="code-block__toolbar-lang" title={label}>
        {label}
      </span>
      <div className="code-block__toolbar-actions">
        <CopyButton text={value} className="code-block__toolbar-copy" />
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
      </div>
    </div>
  );
}
