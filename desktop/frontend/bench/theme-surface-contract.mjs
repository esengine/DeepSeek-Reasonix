#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium } = await import("playwright");

const styles = fs.readFileSync(path.join(frontendDir, "src", "styles.css"), "utf8");
const themeStyles = ["graphite", "aurora", "slate", "carbon", "nocturne", "amber"];
const modes = [
  { name: "forced dark", attribute: "dark", media: "dark" },
  { name: "forced light", attribute: "light", media: "dark" },
  { name: "auto dark", attribute: null, media: "dark" },
  { name: "auto light", attribute: null, media: "light" },
];
const overlaySelectors = [
  ".anchored-popover",
  ".slashmenu",
  ".composer-access-menu",
  ".jobs-popover",
  ".modelsw__menu",
  ".workspace-recent-menu",
  ".floating-menu",
  ".mem-ws-select__menu",
  ".menu",
  ".sound-select__menu",
  ".settings-model-picker__menu",
  ".external-opener__menu",
  ".context-menu",
  ".theme-gallery__menu",
  ".theme-gallery__editor",
  ".theme-editor__header",
  ".theme-editor__live",
  ".provider-access-more__menu",
  ".topicbar__export-menu",
  ".heartbeat-filter-menu",
  ".heartbeat-project-menu",
  ".heartbeat-task-menu",
];
const surfaceSelectors = [
  ".msg-pasted-block",
  ".composer__pasted-block",
  ".mem-tab-select",
  ".mermaid-diagram__error",
];
const fixtures = [...new Set([...overlaySelectors, ...surfaceSelectors])]
  .map((selector) => `<div class="${selector.slice(1)}">surface</div>`)
  .join("");

const amberExpected = {
  dark: {
    "--bg": "#090a0c",
    "--bg-soft": "#111319",
    "--panel": "#191b22",
    "--sidebar-bg": "#0c0e12",
    "--fg": "#f4f5f7",
    "--fg-dim": "#c0c4cc",
    "--accent": "#d4632f",
    "--border": "#343945",
  },
  light: {
    "--bg": "#f7f8fb",
    "--bg-soft": "#eef2f7",
    "--panel": "#ffffff",
    "--sidebar-bg": "#f3f6fa",
    "--fg": "#111827",
    "--fg-dim": "#4b5563",
    "--accent": "#dd5b28",
    "--border": "#d8dee8",
  },
};

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS  ${message}\n`);
}

let browser;
try {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.setContent(`<!doctype html><html><head><style>${styles}</style></head><body>${fixtures}<div class="transcript__loading">muted</div></body></html>`);

  for (const style of themeStyles) {
    for (const mode of modes) {
      await page.emulateMedia({ colorScheme: mode.media });
      await page.evaluate(({ style, attribute }) => {
        const root = document.documentElement;
        root.setAttribute("data-theme-style", style);
        if (attribute) root.setAttribute("data-theme", attribute);
        else root.removeAttribute("data-theme");
        root.removeAttribute("data-theme-pack");
        root.removeAttribute("data-theme-has-bg");
        root.removeAttribute("data-theme-scene");
        root.style.removeProperty("--theme-pane-overlay-pct");
        root.style.removeProperty("--theme-pane-task-overlay-pct");
      }, { style, attribute: mode.attribute });

      const result = await page.evaluate(({ overlaySelectors, surfaceSelectors }) => {
        function colorAlpha(value) {
          const canvas = document.createElement("canvas");
          canvas.width = 1;
          canvas.height = 1;
          const context = canvas.getContext("2d");
          context.clearRect(0, 0, 1, 1);
          context.fillStyle = value;
          context.fillRect(0, 0, 1, 1);
          return context.getImageData(0, 0, 1, 1).data[3] / 255;
        }
        const alpha = (selector) => colorAlpha(getComputedStyle(document.querySelector(selector)).backgroundColor);
        const muted = document.querySelector(".transcript__loading");
        const probe = document.createElement("span");
        probe.style.color = "var(--fg-faint)";
        document.body.append(probe);
        const mutedColor = getComputedStyle(muted).color;
        const expectedMutedColor = getComputedStyle(probe).color;
        probe.remove();
        return {
          overlays: Object.fromEntries(overlaySelectors.map((selector) => [selector, alpha(selector)])),
          surfaces: Object.fromEntries(surfaceSelectors.map((selector) => [selector, alpha(selector)])),
          mutedColor,
          expectedMutedColor,
        };
      }, { overlaySelectors, surfaceSelectors });

      for (const [selector, alpha] of Object.entries(result.overlays)) {
        assert(alpha >= 0.999, `${style} ${mode.name} keeps ${selector} opaque (${alpha})`);
      }
      for (const [selector, alpha] of Object.entries(result.surfaces)) {
        assert(alpha > 0, `${style} ${mode.name} resolves ${selector} background (${alpha})`);
      }
      assert(result.mutedColor === result.expectedMutedColor, `${style} ${mode.name} resolves muted text to --fg-faint`);

      if (style === "amber") {
        const expected = amberExpected[mode.media === "light" || mode.attribute === "light" ? "light" : "dark"];
        const colors = await page.evaluate((entries) => {
          const result = {};
          for (const [token] of entries) {
            const probe = document.createElement("span");
            probe.style.backgroundColor = `var(${token})`;
            document.body.append(probe);
            result[token] = getComputedStyle(probe).backgroundColor;
            probe.remove();
          }
          return result;
        }, Object.entries(expected));
        const expectedColors = await page.evaluate((entries) => {
          const result = {};
          for (const [token, value] of entries) {
            const probe = document.createElement("span");
            probe.style.backgroundColor = value;
            document.body.append(probe);
            result[token] = getComputedStyle(probe).backgroundColor;
            probe.remove();
          }
          return result;
        }, Object.entries(expected));
        for (const token of Object.keys(expected)) {
          assert(colors[token] === expectedColors[token], `Amber ${mode.name} ${token} matches preview (${colors[token]})`);
        }
      }
    }
  }

  await page.emulateMedia({ colorScheme: "dark" });
  for (const [paneOpacity, expectedAlpha] of [[0, 0.4], [0.5, 0.9], [1, 1]]) {
    for (const scene of ["home", "task"]) {
      const alphas = await page.evaluate(({ overlaySelectors, paneOpacity, scene }) => {
        const root = document.documentElement;
        const percentage = `${Math.min((paneOpacity + 0.4) * 100, 100)}%`;
        root.setAttribute("data-theme-style", "graphite");
        root.setAttribute("data-theme", "dark");
        root.setAttribute("data-theme-pack", "surface-contract");
        root.setAttribute("data-theme-has-bg", "true");
        root.setAttribute("data-theme-scene", scene);
        root.style.setProperty("--theme-pane-overlay-pct", percentage);
        root.style.setProperty("--theme-pane-task-overlay-pct", percentage);
        return Object.fromEntries(overlaySelectors.map((selector) => {
          const value = getComputedStyle(document.querySelector(selector)).backgroundColor;
          const canvas = document.createElement("canvas");
          canvas.width = 1;
          canvas.height = 1;
          const context = canvas.getContext("2d");
          context.clearRect(0, 0, 1, 1);
          context.fillStyle = value;
          context.fillRect(0, 0, 1, 1);
          return [selector, context.getImageData(0, 0, 1, 1).data[3] / 255];
        }));
      }, { overlaySelectors, paneOpacity, scene });
      for (const [selector, alpha] of Object.entries(alphas)) {
        assert(
          Math.abs(alpha - expectedAlpha) <= 1 / 255,
          `${scene} ${selector} follows paneOpacity ${paneOpacity} (${alpha})`,
        );
      }
    }
  }

  process.stdout.write("\nTheme surface browser contract passed.\n");
} finally {
  await browser?.close();
}
