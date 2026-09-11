// Publishes the shell bar's measured height as --app-bar-height on the document
// root. The bar is the layout's first grid row and sizes to its own content, so
// the two consumers that position themselves against its bottom edge — the
// sidebar resizer and the overlay dock — cannot read the height from CSS. It is
// not a constant: it differs per layout style, theme skin and platform caption.
import { useEffect } from "react";

const VAR = "--app-bar-height";

export function useTopicbarHeightVar(): void {
  useEffect(() => {
    const publish = () => {
      const bar = document.querySelector<HTMLElement>(".topicbar");
      if (!bar) return;
      const height = Math.round(bar.getBoundingClientRect().height);
      if (height > 0) document.documentElement.style.setProperty(VAR, `${height}px`);
    };
    publish();
    const bar = document.querySelector<HTMLElement>(".topicbar");
    if (!bar || typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", publish);
      return () => window.removeEventListener("resize", publish);
    }
    const observer = new ResizeObserver(publish);
    observer.observe(bar);
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty(VAR);
    };
  }, []);
}
