// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { patchSource } from "./htmlsource";

const PAGE = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Demo</title>
  <style>h1 > b { color: red }</style>
</head>
<body>
  <!-- <h1 id="ghost">not an element</h1> -->
  <h1 id="title" class="big">Hello <b>world</b></h1>
  <p id="note">Plain &amp; simple</p>
  <p data-testid="styled" style="color: red; margin: 0">Styled</p>
  <ul><li>one<li>two</ul>
  <script>if (1 < 2) document.title = "<h1 id='x'>"</script>
</body>
</html>
`;

describe("patchSource", () => {
  it("replaces the text of an element and leaves every other byte alone", () => {
    const out = patchSource(PAGE, { selector: "#note", tagName: "p" }, { kind: "text", before: "Plain & simple", after: "Fish & <chips>" });
    expect(out).toEqual({ ok: true, content: PAGE.replace("Plain &amp; simple", "Fish &amp; &lt;chips&gt;") });
  });

  it("refuses text whose current words are not the ones the person saw", () => {
    const out = patchSource(PAGE, { selector: "#note", tagName: "p" }, { kind: "text", before: "something else", after: "x" });
    expect(out).toEqual({ ok: false, code: "mismatch" });
  });

  it("refuses text edits on an element that has children of its own", () => {
    const out = patchSource(PAGE, { selector: "#title", tagName: "h1" }, { kind: "text", before: "Hello world", after: "Bye" });
    expect(out).toEqual({ ok: false, code: "not_plain_text" });
  });

  it("refuses an element whose end tag is implied", () => {
    const out = patchSource(PAGE, { selector: "li:first-child", tagName: "li" }, { kind: "text", before: "one", after: "1" });
    expect(out).toEqual({ ok: false, code: "unmappable" });
  });

  it("adds an inline style where the element has none", () => {
    const out = patchSource(PAGE, { selector: "#note", tagName: "p" }, { kind: "style", property: "color", value: "#336699" });
    expect(out).toEqual({ ok: true, content: PAGE.replace('<p id="note">', '<p style="color: #336699" id="note">') });
  });

  it("replaces one declaration of an existing style and keeps the rest", () => {
    const out = patchSource(PAGE, { selector: '[data-testid="styled"]', tagName: "p" }, { kind: "style", property: "color", value: "blue" });
    expect(out).toEqual({ ok: true, content: PAGE.replace('style="color: red; margin: 0"', 'style="color: blue; margin: 0"') });
  });

  it("appends a declaration the style did not have, writing camelCase as CSS", () => {
    const out = patchSource(PAGE, { selector: '[data-testid="styled"]', tagName: "p" }, { kind: "style", property: "backgroundColor", value: "#fff" });
    expect(out).toEqual({ ok: true, content: PAGE.replace('style="color: red; margin: 0"', 'style="color: red; margin: 0; background-color: #fff"') });
  });

  it("refuses a value that could leave the declaration", () => {
    for (const value of ['red"; x="', "red; display: none", "a{b}", "<b>", "x\ny"]) {
      const out = patchSource(PAGE, { selector: "#note", tagName: "p" }, { kind: "style", property: "color", value });
      expect(out).toEqual({ ok: false, code: "bad_value" });
    }
  });

  it("does not read markup inside comments or scripts as elements", () => {
    const out = patchSource(PAGE, { selector: "#ghost", tagName: "h1" }, { kind: "style", property: "color", value: "red" });
    expect(out).toEqual({ ok: false, code: "not_found" });
    const next = patchSource(PAGE, { selector: "#note", tagName: "p" }, { kind: "style", property: "color", value: "red" });
    expect(next.ok).toBe(true);
  });

  it("refuses a selector naming a different tag than the pick reported", () => {
    const out = patchSource(PAGE, { selector: "#note", tagName: "div" }, { kind: "style", property: "color", value: "red" });
    expect(out).toEqual({ ok: false, code: "not_found" });
  });

  it("refuses a selector that names more than one element", () => {
    const out = patchSource(PAGE, { selector: "p", tagName: "p" }, { kind: "style", property: "color", value: "red" });
    expect(out).toEqual({ ok: false, code: "ambiguous" });
  });

  it("refuses a page the parser rebuilt beyond what the source says", () => {
    const table = "<table><tr><td id=c>x</td></tr></table>";
    const ok = patchSource(table, { selector: "#c", tagName: "td" }, { kind: "style", property: "color", value: "red" });
    expect(ok).toEqual({ ok: true, content: '<table><tr><td style="color: red" id=c>x</td></tr></table>' });
    const broken = patchSource("<b><p id=q>y</b>", { selector: "#q", tagName: "p" }, { kind: "style", property: "color", value: "red" });
    expect(broken).toEqual({ ok: false, code: "unmappable" });
  });

  it("refuses an invalid selector instead of throwing", () => {
    const out = patchSource(PAGE, { selector: "###", tagName: "p" }, { kind: "style", property: "color", value: "red" });
    expect(out).toEqual({ ok: false, code: "not_found" });
  });
});
