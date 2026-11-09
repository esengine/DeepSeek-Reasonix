import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { ServiceMark } from "./ServiceMark";

it("shows a service icon for an unknown provider instead of a letter or emoji", () => {
  const html = renderToStaticMarkup(<ServiceMark id="🚀 custom" />);
  expect(html).toContain("lucide-server");
  expect(html).toContain('aria-hidden="true"');
  expect(html).not.toContain(">R<");
});
