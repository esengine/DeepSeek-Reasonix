import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { CopyButton } from "./CopyButton";

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
    const fence = "```";
    const lang = language?.trim() ?? "";
    const body = `${fence}${lang}\n${value.replace(/\n$/, "")}\n${fence}\n`;
    try {
      await navigator.clipboard.writeText(body);
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
