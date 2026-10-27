import { expect, it } from "vitest";

const CSS = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./studio.css"]!;

// The shell floats macOS's window controls over the page corner, 11px from the
// left, and they run past the icon rail's width. A rail edge or a rail
// background that reaches the top of the window cuts straight through them.
const rules = (selector: RegExp) => [...css.matchAll(selector)].map((m) => m[1]!).join("\n");

it("starts the icon rail's edge below the title bar where the window controls float", () => {
  const rule = rules(/\[data-titlebar="app"\]\[data-platform="darwin"\] \.app\[data-nav="on"\] \.nav\s*\{([^}]*)\}/g);
  expect(rule).toMatch(/border-right-width:\s*0/);
  expect(rule).toMatch(/background-size:\s*1px calc\(100% - var\(--chrome-h\)\)/);
  expect(rule).toMatch(/background-position:\s*right bottom/);
});

it("lets the rail's corner take the page colour only while the sidebar is closed, so an open sidebar's header stays one band with it", () => {
  const closed = rules(/\[data-titlebar="app"\]\[data-platform="darwin"\] \.app\[data-nav="on"\]\[data-rail="off"\] \.nav\s*\{([^}]*)\}/g);
  expect(closed).toMatch(/background-color:\s*transparent/);
  expect(closed).toMatch(/100% calc\(100% - var\(--chrome-h\)\)/);
  const always = rules(/\[data-titlebar="app"\]\[data-platform="darwin"\] \.app\[data-nav="on"\] \.nav\s*\{([^}]*)\}/g);
  expect(always).not.toMatch(/background-color/);
});

it("leaves the rail as it was on every platform and shell that does not float the window controls", () => {
  const plain = css.match(/\.nav\s*\{([^}]*border-right-color[^}]*)\}/)?.[1] ?? "";
  expect(plain).toMatch(/background:\s*color-mix/);
  expect(plain).not.toMatch(/background-size/);
});
