import { lazy, memo, Suspense } from "react";

const MarkdownRenderer = lazy(() => import("./MarkdownRenderer"));

export const Markdown = memo(function Markdown({
  text,
}: {
  text: string;
}) {
  return (
    <Suspense fallback={<div className="md">{text}</div>}>
      <MarkdownRenderer text={text} />
    </Suspense>
  );
});
