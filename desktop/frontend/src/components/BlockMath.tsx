import katex from "katex";
import { CopyButton } from "./CopyButton";
import { latexNormalizeForKatex } from "./latexNormalize";

interface BlockMathProps {
  source: string;
}

export function BlockMath({ source }: BlockMathProps) {
  const normalized = latexNormalizeForKatex(source);

  let html: string;
  try {
    html = katex.renderToString(normalized, {
      throwOnError: false,
      displayMode: true,
    });
  } catch {
    return <pre className="block-math-fallback"><code>{source}</code></pre>;
  }

  return (
    <div className="block-math">
      <CopyButton text={source} className="block-math__copy" />
      <div dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  );
}
