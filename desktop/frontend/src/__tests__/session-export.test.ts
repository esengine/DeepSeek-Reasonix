import assert from "node:assert/strict";
import {
  createRasterPdf,
  isSafeInlineExportImage,
  neutralizeExternalCssResources,
  planRasterSlices,
  transformExportMarkdownUrl,
} from "../lib/sessionExportCore";

const css = neutralizeExternalCssResources(`
  @font-face { font-family: Test; src: url("fonts/test.woff2") format("woff2"); }
  .safe { color: red; background-image: url(https://example.com/image.png); }
`);
assert.equal(css.includes("url("), false);
assert.match(css, /\.safe \{ color: red;/);

assert.deepEqual(planRasterSlices(25_000, 8_000), [
  { offset: 0, height: 8_000 },
  { offset: 8_000, height: 8_000 },
  { offset: 16_000, height: 8_000 },
  { offset: 24_000, height: 1_000 },
]);
assert.deepEqual(planRasterSlices(Number.NaN, 0), [{ offset: 0, height: 1 }]);
assert.deepEqual(planRasterSlices(25_000, 8_000, [7_000, 14_200, 23_000]), [
  { offset: 0, height: 7_000 },
  { offset: 7_000, height: 7_200 },
  { offset: 14_200, height: 8_000 },
  { offset: 22_200, height: 2_800 },
]);
assert.deepEqual(planRasterSlices(1_400, 1_354, [1_352], 1_352), [
  { offset: 0, height: 1_354 },
]);
assert.deepEqual(planRasterSlices(1_048, 1_000, [1_000], 1_000), [
  { offset: 0, height: 1_000 },
]);
assert.deepEqual(planRasterSlices(2_048, 1_000, [900, 1_950], 1_950), [
  { offset: 0, height: 900 },
  { offset: 900, height: 1_000 },
  { offset: 1_900, height: 148 },
]);

assert.equal(isSafeInlineExportImage("data:image/png;base64,iVBORw0KGgo="), true);
assert.equal(isSafeInlineExportImage(" DATA:IMAGE/JPEG;BASE64,/9j/4AAQ "), true);
assert.equal(isSafeInlineExportImage("data:image/webp;base64,UklGRg=="), true);
assert.equal(isSafeInlineExportImage("data:image/gif;base64,R0lGODlh"), true);
assert.equal(isSafeInlineExportImage("https://example.com/image.png"), false);
assert.equal(isSafeInlineExportImage("file:///tmp/image.png"), false);
assert.equal(isSafeInlineExportImage("data:image/svg+xml;base64,PHN2Zz4="), false);
assert.equal(isSafeInlineExportImage("data:image/png,not-base64"), false);
assert.equal(isSafeInlineExportImage("data:image/png;base64,not base64"), false);
assert.equal(isSafeInlineExportImage("data:image/png;base64,invalid!"), false);

const fallbackUrlTransform = (value: string) => `filtered:${value}`;
assert.equal(
  transformExportMarkdownUrl(" data:image/png;base64,iVBORw0KGgo= ", "src", fallbackUrlTransform),
  "data:image/png;base64,iVBORw0KGgo=",
);
assert.equal(
  transformExportMarkdownUrl("data:image/png;base64,iVBORw0KGgo=", "href", fallbackUrlTransform),
  "filtered:data:image/png;base64,iVBORw0KGgo=",
);
assert.equal(
  transformExportMarkdownUrl("https://example.com/image.png", "src", fallbackUrlTransform),
  "filtered:https://example.com/image.png",
);

const pdf = createRasterPdf(
  [
    { bytes: new Uint8Array([0xff, 0xd8, 0xff, 0xd9]), width: 100, height: 120 },
    { bytes: new Uint8Array([0xff, 0xd8, 0xff, 0xd9]), width: 100, height: 80 },
  ],
  "Export test",
);
const pdfText = new TextDecoder("latin1").decode(pdf);
assert.equal(pdfText.startsWith("%PDF-1.4"), true);
assert.match(pdfText, /\/Count 2/);
assert.equal((pdfText.match(/\/Subtype \/Image/g) ?? []).length, 2);
assert.match(pdfText, /startxref\n\d+\n%%EOF/);

console.log("session export tests passed");
