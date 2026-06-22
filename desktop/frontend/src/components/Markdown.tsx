import { lazy, memo, Suspense } from "react";

const MarkdownRenderer = lazy(() => import("./MarkdownRenderer"));

export const Markdown = memo(function Markdown({
  text,
  plainStatusBlocks = false,
  onFileLinkClick,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  onFileLinkClick?: (path: string) => void;
}) {
  return (
    <Suspense fallback={<div className="md">{text}</div>}>
      <MarkdownRenderer text={text} plainStatusBlocks={plainStatusBlocks} onFileLinkClick={onFileLinkClick} />
    </Suspense>
  );
});
