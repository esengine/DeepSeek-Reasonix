// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Group } from "./Group";

afterEach(cleanup);

describe("a settings block says what it does differently from the page's usual", () => {
  it("says nothing beside the name of a block that applies on this machine at once", () => {
    const { container } = render(<Group id="ask-timeout" title="等待时间" hint="x">y</Group>);
    expect(container.querySelector(".scope-tag")).toBeNull();
    expect(container.querySelector(".apply")?.hasAttribute("data-usual")).toBe(true);
  });

  it("tags a block that needs a restart", () => {
    const { container } = render(<Group id="language" title="语言" hint="x">y</Group>);
    expect(container.querySelector(".scope-tag")?.textContent).toBe("重启后生效");
    expect(container.querySelector(".apply")?.hasAttribute("data-usual")).toBe(false);
  });

  it("tags a block that belongs to the workspace and not the machine", () => {
    const { container } = render(<Group id="session-dir" title="目录" hint="x">y</Group>);
    expect(container.querySelector(".scope-tag")?.textContent).toBe("当前工作区");
  });

  it("keeps the whole description as the hover text of a one-line hint", () => {
    const { container } = render(<Group id="ask-timeout" title="等待时间" hint="很长的一句说明">y</Group>);
    expect(container.querySelector(".hint")?.getAttribute("title")).toBe("很长的一句说明");
  });
});
