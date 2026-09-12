import assert from "node:assert/strict";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { LocaleProvider } from "../lib/i18n";
import { PresentedFiles } from "../components/PresentedFiles";
import type { PresentedFileView } from "../lib/chatViewSource";

const files: PresentedFileView[] = Array.from({ length: 5 }, (_, index) => ({
  path: `output/file-${index + 1}.html`,
  description: `Preview ${index + 1}`,
  toolCallId: `present-${index + 1}`,
}));

const markup = renderToStaticMarkup(
  <LocaleProvider><PresentedFiles files={files} tabId="tab-1" /></LocaleProvider>,
);

assert.equal((markup.match(/class="presented-file"/g) ?? []).length, 4);
assert.match(markup, /file-1\.html/);
assert.doesNotMatch(markup, /file-5\.html/);
assert.match(markup, /5/);
console.log("presented files UI: four-card collapsed grid passed");
