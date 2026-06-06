import katex from "katex";
import { useState } from "react";
import { CopyButton } from "./CopyButton";
import { latexNormalizeForKatex } from "./latexNormalize";

interface InlineMathProps {
  source: string;
}

export function InlineMath({ source }: InlineMathProps) {
  const [showSource, setShowSource] = useState(false);

  const normalized = latexNormalizeForKatex(source);
  const html = katex.renderToString(normalized, {
    throwOnError: false,
    displayMode: false,
  });

  return (
    <span
      className="inline-math"
      onMouseEnter={() => setShowSource(true)}
      onMouseLeave={() => setShowSource(false)}
    >
      <span dangerouslySetInnerHTML={{ __html: html }} />
      {showSource && (
        <span className="inline-math__source">
          <code>{source}</code>
          <CopyButton text={source} />
        </span>
      )}
    </span>
  );
}
