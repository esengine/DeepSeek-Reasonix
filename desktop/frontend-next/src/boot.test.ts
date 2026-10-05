import { describe, expect, it } from "vitest";
import HTML from "../index.html?raw";

// The rules the boot screen carries paint before the bundle that would style
// anything exists, so nothing else is watching them: perf.html is a separate
// entry, and every browser guard under perf/ loads that one.
describe("the shipped entry", () => {
  const inline = HTML.slice(HTML.indexOf("<style>"), HTML.lastIndexOf("</style>"));

  it("leaves the root element's background alone", () => {
    // A theme's picture is body::before at z-index -2, visible only because an
    // uncoloured html hands the canvas to body's background. Colouring html
    // takes the canvas over and demotes body's to an ordinary element paint,
    // which lands on top of the picture.
    const offenders: string[] = [];
    for (const rule of inline.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const selectors: string = rule[1];
      const declarations: string = rule[2];
      const onRoot = selectors.split(",").some((one: string) => /(^|\s)(html|:root)\s*$/.test(one));
      if (onRoot && /(^|;)\s*background(-color)?\s*:/.test(declarations)) offenders.push(selectors.trim());
    }
    expect(offenders, "html/:root must not set a background").toEqual([]);
  });

  it("paints the boot screen itself opaque", () => {
    expect(inline).toMatch(/\.boot\s*\{[^}]*background:\s*var\(--boot-bg\)/);
  });

  it("marks each real startup step and names the one still pending", () => {
    // One mark per step intro.progress can report: bundle, kernel, app.
    expect(HTML).toContain('<div class="bsteps" aria-hidden="true"><i></i><i></i><i></i></div>');
    for (const at of ["1", "2"]) {
      expect(HTML).toMatch(new RegExp(`<span data-at="${at}"><span data-zh>[^<]+</span><span data-en>[^<]+</span></span>`));
    }
  });

  it("still shows the wordmark when the intro never runs", () => {
    // The letters start hidden for the intro to take; a bundle that fails to
    // load must not leave the window a blank night sky.
    expect(inline).toMatch(/\.bmark \.bl\s*\{[^}]*animation:\s*bootfallback/);
    expect(inline).toMatch(/\.boot\[data-intro\] \.bmark \.bl\s*\{\s*animation:\s*none/);
  });
});

// A shared serve link carries its credential in the fragment, which the browser
// never sends. The kernel's own pages used to trade it for the cookie; they are
// gone, so this page does it — before the bundle's first request, and without
// leaving the token in the address bar for the next person to copy.
describe("a credential arriving in the link", () => {
  it("is traded for the cookie and wiped from the address bar", () => {
    for (const want of [
      "window.location.hash.slice(1)",
      '"/auth/token"',
      "window.history.replaceState",
      "window.fetch =",
    ]) {
      expect(HTML).toContain(want);
    }
  });
});
