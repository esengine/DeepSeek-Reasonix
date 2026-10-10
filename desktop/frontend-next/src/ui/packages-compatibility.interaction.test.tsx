// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import type { AgentPort, PluginPackage } from "../port/port";

afterEach(cleanup);

it("replaces repeated compatibility issues without retaining obsolete rows", () => {
  const prompt = 'unsupported hook type "prompt" for SessionStart';
  const agent = 'unsupported hook type "agent" for SessionStart';
  const skipped = Array.from({ length: 3 }, () => ({ capability: "hooks", reason: prompt }));
  const pkg: PluginPackage = {
    name: "compat-kit", root: "/fixture/compat-kit", enabled: true, skipped,
    skills: [{ name: "greet", description: "Local fixture", invocation: "/compat-kit:greet" }],
  };
  const props = { port: new MockPort() as unknown as AgentPort, onChanged: vi.fn(), updating: "", onUpdate: vi.fn() };
  const view = render(<Packages {...props} packages={[pkg]} />);
  const reasons = () => Array.from(view.container.querySelectorAll(".peek .sc"), (row) => row.textContent);
  expect(reasons()).toEqual(["Local fixture", ...Array(3).fill(`用不了：${prompt}`)]);

  view.rerender(<Packages {...props} packages={[{ ...pkg, skipped: [{ capability: "hooks", reason: agent }] }]} />);
  expect(reasons()).toEqual(["Local fixture", `用不了：${agent}`]);

  view.rerender(<Packages {...props} packages={[{ ...pkg, skipped }]} />);
  expect(reasons()).toEqual(["Local fixture", ...Array(3).fill(`用不了：${prompt}`)]);
  view.rerender(<Packages {...props} packages={[{ ...pkg, skipped: [] }]} />);
  expect(reasons()).toEqual(["Local fixture"]);
});
