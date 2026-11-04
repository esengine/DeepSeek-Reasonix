// @vitest-environment jsdom
import { expect, it } from "vitest";
import { sanitizeMermaidSvg } from "./mermaidSvg";

it("preserves real diagram geometry, labels, themes and local arrow markers", () => {
  const svg = sanitizeMermaidSvg('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><style>#graph .node{fill:#eee;stroke:#333} .edge{marker-end:url(#arrow)}</style><defs><marker id="arrow"><path d="M0 0 L5 5"/></marker></defs><g class="node" transform="translate(20,30)"><rect width="100" height="40"/><text>Browser</text></g><path class="edge" d="M0 0 L100 100"/></svg>');
  expect(svg).toContain('viewBox="0 0 200 100"');
  expect(svg).toContain('width="200"');
  expect(svg).toContain('height="100"');
  expect(svg).toContain('marker-end:url(#arrow)');
  expect(svg).toContain('translate(20,30)');
  expect(svg).toContain("Browser");
});

it("removes scripts, handlers, HTML, links, external images and CSS resource loads", () => {
  const svg = sanitizeMermaidSvg(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)">
    <script>alert(1)</script><foreignObject><iframe src="https://evil.test"/></foreignObject>
    <image href="https://evil.test/leak"/><use href="https://evil.test/icons.svg#x"/>
    <a href="javascript:alert(1)" onclick="alert(1)"><text>label</text></a>
    <a href="https://evil.test/"><text>web</text></a>
    <style>@import "https://evil.test/a.css";.x{fill:url(https://evil.test/a);stroke:red}.y{fill:u\\72l(https://evil.test/b)}</style>
    <rect style="fill:url(https://evil.test/c);stroke:blue" onmouseover="alert(1)"/>
    <animate attributeName="href" to="https://evil.test/"/>
  </svg>`);
  expect(svg).not.toMatch(/evil|javascript|onload|onclick|onmouseover|<script|foreignObject|<image|<use|<animate|@import/i);
  expect(svg).toContain("label");
  expect(svg).toContain("stroke:blue");
});

it("fails closed for non-SVG or malformed output", () => {
  expect(() => sanitizeMermaidSvg("<html><body>not svg</body></html>")).toThrow();
  expect(() => sanitizeMermaidSvg('<svg><g></svg>')).toThrow();
});
