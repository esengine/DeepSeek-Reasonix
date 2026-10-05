import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { CommandMode } from "./Sandbox";
import type { SandboxSettings } from "../port/port";

const jail = (over: Partial<SandboxSettings> = {}): SandboxSettings => ({
  bash: "",
  network: true,
  workspaceRoot: "",
  allowWrite: [],
  protectChangedFiles: true,
  effectiveWriteRoots: ["/Users/you/code/site"],
  effectiveBash: "enforce",
  available: true,
  platform: "darwin",
  path: "/Users/you/.reasonix/config.toml",
  ...over,
});

// A Windows host, which has no OS-level backend at all.
const windows = (over: Partial<SandboxSettings> = {}) =>
  jail({
    effectiveBash: "off",
    available: false,
    platform: "windows",
    whyCode: "sandbox.unsupported_on_windows",
    ...over,
  });

const draw = (box: SandboxSettings) =>
  renderToStaticMarkup(<CommandMode box={box} busy="" onPick={() => {}} onNetwork={() => {}} />);

// segment returns the option carrying a label, so an assertion names the choice
// it is about rather than an index into the markup.
function segment(markup: string, label: string): string {
  for (const m of markup.matchAll(/<button[^>]*>([^<]*)<\/button>/g)) {
    if (m[1] === label) return m[0];
  }
  return "";
}

describe("how commands run", () => {
  // The reported defect: `disabled` alone is invisible here — :hover still
  // matches a disabled button, so the dead option lit up under the pointer and
  // then swallowed the click. It read as broken rather than as unavailable.
  it("marks the mode this host cannot run, and hangs the reason on it", () => {
    const enforce = segment(draw(windows()), "关进沙箱");
    expect(enforce, "the mode is still shown, so a reader learns it exists").toBeTruthy();
    expect(enforce).toContain("disabled");
    expect(enforce, "a host that can never jail is not a save in flight").toContain("data-locked");
    expect(enforce, "the warning above is commentary until the option points at it").toContain("aria-describedby");
  });

  // Nothing configured is not "off": it enforces everywhere but Windows. The
  // pane reports a security posture, so it draws the mode that will run.
  it("selects what will run rather than what the file says", () => {
    const unset = draw(jail());
    expect(segment(unset, "关进沙箱")).toContain('aria-checked="true"');
    expect(segment(unset, "不受限")).toContain('aria-checked="false"');
  });

  it("says so when the file asked for a mode this machine overrules", () => {
    const overruled = draw(windows({ bash: "enforce" }));
    expect(segment(overruled, "不受限")).toContain('aria-checked="true"');
    expect(overruled, "a config synced from a mac still says enforce").toContain("实际生效");
    expect(draw(windows()), "an unset mode agrees with off and is not a disagreement").not.toContain("实际生效");
  });

  // Egress hangs off the jail that runs, not off the word in the file: reading
  // the file would hide the only control over a live sandbox's network.
  it("offers the egress switch whenever the jail is the thing that runs", () => {
    expect(draw(jail())).toContain("沙箱里允许联网");
    expect(draw(windows())).not.toContain("沙箱里允许联网");
  });
});
