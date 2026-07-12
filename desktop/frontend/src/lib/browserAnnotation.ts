import type { BrowserElementView, BrowserSessionView } from "./bridge";
import type { BrowserAnnotation } from "./types";

function compactText(value: string | undefined, limit = 160): string {
  return (value ?? "").replace(/\s+/g, " ").trim().slice(0, limit);
}

export function browserStyleChanges(
  selection: BrowserElementView,
): BrowserAnnotation["styleChanges"] {
  return Object.fromEntries(
    Object.entries(selection.styleOverrides ?? {})
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([name, after]) => [
        name,
        {
          before: selection.computedStyles[name] ?? "",
          after,
        },
      ]),
  );
}

export function createBrowserAnnotation(
  selection: BrowserElementView,
  session: BrowserSessionView,
  screenshotPath?: string,
  elementScreenshotPath?: string,
): BrowserAnnotation {
  return {
    page: {
      url: selection.url || session.url,
      title: selection.title || session.title,
    },
    element: {
      tag: selection.tag,
      selector: selection.selector,
      accessibleName: compactText(selection.accessibleName, 240) || undefined,
      text: compactText(selection.text, 320) || undefined,
      box: selection.box,
    },
    viewport: {
      width: session.width,
      height: session.height,
    },
    styleChanges: browserStyleChanges(selection),
    screenshotPath,
    elementScreenshotPath,
  };
}

export function formatBrowserAnnotation(annotation: BrowserAnnotation): string {
  const label = annotation.element.accessibleName || annotation.element.text;
  const element = label
    ? `${annotation.element.tag} — “${label.replace(/[“”"]/g, "'")}”`
    : annotation.element.tag;
  const box = annotation.element.box;
  const changes = Object.entries(annotation.styleChanges);
  const styleLines = changes.length
    ? changes.map(([name, change]) => `- ${name}: ${change.before || "(empty)"} -> ${change.after}`)
    : ["- No temporary style overrides"];

  return [
    "[Browser annotation]",
    `URL: ${annotation.page.url}`,
    annotation.page.title ? `Title: ${annotation.page.title}` : "",
    `Element: ${element}`,
    `Selector: ${annotation.element.selector}`,
    `Bounds: x=${Math.round(box.x)}, y=${Math.round(box.y)}, width=${Math.round(box.width)}, height=${Math.round(box.height)}`,
    `Viewport: ${annotation.viewport.width}x${annotation.viewport.height}`,
    "Style changes:",
    ...styleLines,
    annotation.screenshotPath ? `Screenshot: ${annotation.screenshotPath}` : "Screenshot: unavailable",
    annotation.elementScreenshotPath ? `Element screenshot: ${annotation.elementScreenshotPath}` : "",
    "Request: 按以上标注修改对应源码样式。",
  ].filter(Boolean).join("\n");
}