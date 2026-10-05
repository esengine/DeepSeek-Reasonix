import { lazy, Suspense } from "react";
import type { LocalRefs } from "./Markdown";

// React.lazy keeps a failed import failed for the life of the page, so one
// chunk that did not load (a dropped connection, a dev server mid-reload, an
// update that replaced the files) would leave every reply as source text. A
// failure swaps in a fresh lazy component, and the next render loads again.
function loader() {
  return lazy(async () => {
    try {
      return { default: (await import("./Markdown")).Markdown };
    } catch (err) {
      Markdown = loader();
      throw err;
    }
  });
}

let Markdown = loader();

export function LazyMarkdown({ text, streaming, local }: { text: string; streaming?: boolean; local?: LocalRefs }) {
  return (
    <Suspense fallback={<div className="md" style={{ whiteSpace: "pre-wrap" }}>{text}</div>}>
      <Markdown text={text} streaming={streaming} local={local} />
    </Suspense>
  );
}
