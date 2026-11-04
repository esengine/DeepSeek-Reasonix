// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { SettingsTitle } from "./SettingsNav";

afterEach(cleanup);

it("names the page, how many settings it holds and what it is for", () => {
  const { container } = render(<SettingsTitle at="model" />);
  const bar = container.querySelector(".prefs-pagetitle")!;
  expect(bar.querySelector("h2")?.textContent).toBe("模型");
  expect(bar.querySelector(".count")?.textContent).toMatch(/^\d+ 项$/);
  expect(bar.querySelector(".desc .what")?.textContent).toBe("接哪个服务、每种用途用哪个模型、上下文怎么维护");
  expect(bar.querySelector(".group")).toBeNull();
});

it("is one heading, so the page has a single labelled landmark", () => {
  const { container } = render(<SettingsTitle at="tools" />);
  expect(container.querySelectorAll("h2")).toHaveLength(1);
  expect(container.querySelector("h2")?.textContent).toBe("权限与安全");
});
