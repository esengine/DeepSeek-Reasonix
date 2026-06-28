import { lazy, memo, Suspense } from "react";

const MarkdownRenderer = lazy(() => import("./MarkdownRenderer"));

// Preload the MarkdownRenderer code-split chunk so the first message renders
// without a Suspense fallback flash of raw text.
let preloaded = false;
function preloadMarkdownRenderer() {
  if (preloaded) return;
  preloaded = true;
  void import("./MarkdownRenderer");
}
preloadMarkdownRenderer();

export const Markdown = memo(function Markdown({
  text,
  plainStatusBlocks = false,
}: {
  text: string;
  plainStatusBlocks?: boolean;
}) {
  return (
    <Suspense fallback={<div className="md">{text}</div>}>
      <MarkdownRenderer text={text} plainStatusBlocks={plainStatusBlocks} />
    </Suspense>
  );
});
