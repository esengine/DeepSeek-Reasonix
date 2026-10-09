import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./{app,studio,feedback}.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const sheet = (name: string) => SHEETS[`./${name}.css`];

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
function bodies(css: string, selector: string): string {
  const re = new RegExp(`(?:^|[,\\s])${escape(selector)}\\s*\\{([^}]*)\\}`, "gm");
  return [...css.replace(/[ \t]+/g, " ").matchAll(re)].map((m) => m[1]).join(";");
}

const STATUS_HUES = /var\(--(net|deleg)(-wash)?\)/;

// A decorative role reads its own token; the token's default is the status
// colour it used to borrow, so what is painted does not change.
const DECORATIVE: [string, string, string[]][] = [
  ["app", ".hit-row .u", ["--link"]],
  ["app", ".lk", ["--link"]],
  ["app", ".md a", ["--link"]],
  ["feedback", ".fbk a", ["--link"]],
  ["app", ".hl .who", ["--label-agent", "--label-agent-wash"]],
  ["app", ".nest-hd .who", ["--label-agent"]],
  ["studio", ".agent-tx-hd .who", ["--label-agent"]],
  ["studio", '.chipmirror .skillchip[data-kind="subagent"]', ["--label-agent", "--label-agent-wash"]],
  ["studio", ".chipmirror .skillchip", ["--brand", "--brand-wash"]],
  ["studio", ".studio-brand-accent", []],
  ["studio", ".studio-wallet > span", ["--brand"]],
  ["studio", ':root[data-theme="light"] .studio-wallet > span', ["--brand"]],
  ["studio", ".chrome .browser-action", ["--brand"]],
  ["studio", '.chrome .browser-action[aria-pressed="true"]', ["--brand-wash"]],
  ["studio", ':root[data-theme="light"] .chrome .browser-action', ["--brand"]],
  ["studio", ':root[data-theme="light"] .chrome .browser-action[aria-pressed="true"]', ["--brand-wash"]],
  ["studio", ".flow .opt[data-on]", ["--brand-wash"]],
  ["studio", ':root[data-theme="light"] .flow .opt[data-on]', ["--brand-wash"]],
  ["feedback", '.fbk-note[data-tone="info"] > .studio-icon', ["--brand"]],
  ["feedback", '.fbk-chip[data-status="recorded"]', ["--brand"]],
  ["feedback", '.fbk-chip[data-status="answered"] .studio-icon', ["--brand"]],
  ["feedback", ".fbk-form[data-over] .fbk-drop", ["--halo", "--brand-wash"]],
  ["app", ".ws[aria-selected=\"true\"]", ["--brand"]],
  ["app", ".tshot:hover", ["--halo"]],
  ["app", '.pane[data-intake="ref"] .compose', ["--halo"]],
  ["app", '.pane[data-intake="ref"] .compose::after', ["--halo"]],
  ["app", ".uhero:hover, .utile:hover, .ucard:hover, .storage .item:hover", ["--halo"]],
];

// Running, connected, live and delegated-out say what the agent is doing and
// stay on the status hues, which a pack cannot reach.
const STATUS: [string, string, string][] = [
  ["app", ".caret", "--net"],
  ["app", '.ws[data-s="running"] .pip', "--net"],
  ["app", '.call[data-k="net"] > .g .sym', "--net"],
  ["app", '.call[data-k="deleg"] > .g .sym', "--deleg"],
  ["app", ".nest", "--deleg"],
  ["app", '.rmtrow[data-state="connected"] .st', "--net"],
  ["app", ".call[data-running] > .g .line", "--net"],
  ["app", ".pane[data-run=\"running\"] .compose:focus-within", "--net"],
  ["studio", '.tool-state[data-state="running"]', "--net"],
  ["studio", ".run-live", "--net"],
  ["studio", ".activity-running", "--net"],
  ["studio", ".studio-runstate[data-running] .studio-runlabel", "--net"],
];

describe("decorative roles are not painted with a status hue", () => {
  for (const [file, selector, tokens] of DECORATIVE) {
    it(`${file}: ${selector}`, () => {
      const body = bodies(sheet(file), selector);
      expect(body, "selector not found").not.toBe("");
      expect(body).not.toMatch(STATUS_HUES);
      for (const token of tokens) expect(body).toContain(`var(${token})`);
    });
  }
});

it("keeps composer labels quiet until the pane is running", () => {
  const css = sheet("studio");
  const label = css.match(/^\.studio-runlabel\s*\{([^}]*)\}/m)?.[1] ?? "";
  expect(label).toMatch(/font:\s*inherit/);
  expect(label).not.toMatch(/padding:|background:|font-weight:/);
  expect(bodies(css, ".studio-runstate[data-waiting] .studio-runlabel")).toBe("");
  expect(bodies(css, ".studio-runstate[data-idle] .studio-runlabel")).toBe("");
});

describe("status roles keep the status hues", () => {
  for (const [file, selector, hue] of STATUS) {
    it(`${file}: ${selector}`, () => {
      const body = bodies(sheet(file), selector);
      expect(body, "selector not found").not.toBe("");
      expect(body).toContain(`var(${hue}`);
    });
  }
});
