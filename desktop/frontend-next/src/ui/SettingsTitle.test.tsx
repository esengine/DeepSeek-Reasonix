// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { SettingsTitle } from "./SettingsNav";

afterEach(cleanup);

it("names the page and the group it sits in, so a long page keeps its place", () => {
  const { container } = render(<SettingsTitle at="session" />);
  const bar = container.querySelector(".prefs-pagetitle")!;
  expect(bar.querySelector(".group")?.textContent).toBe("本轮执行");
  expect(bar.querySelector("h2")?.textContent).toBe("会话");
});

it("is one heading, so the page has a single labelled landmark", () => {
  const { container } = render(<SettingsTitle at="providers" />);
  expect(container.querySelectorAll("h2")).toHaveLength(1);
  expect(container.querySelector("h2")?.textContent).toBe("模型服务");
});
