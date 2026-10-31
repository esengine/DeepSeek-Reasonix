// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(cleanup);

function hub() {
  const h = new MockHub();
  const build = h.portFor.bind(h);
  h.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    port.welcomeSeen = async () => true;
    return port;
  };
  return h;
}

it("the scope row holds only its four filters; the find box above the list is the one name search", async () => {
  render(<App hub={hub()} />);
  const group = await screen.findByRole("group", { name: "会话范围" });
  expect(within(group).getAllByRole("button").map((b) => b.getAttribute("data-value"))).toEqual(["all", "live", "pinned", "archived"]);
  expect(screen.queryByRole("button", { name: "按名称筛选会话" })).toBeNull();
  expect(screen.getAllByRole("searchbox", { name: "搜索会话 / 项目" })).toHaveLength(1);
});

it("a scope filter still presses in and the find box still takes a query", async () => {
  render(<App hub={hub()} />);
  const group = await screen.findByRole("group", { name: "会话范围" });
  const pinned = within(group).getByRole("button", { name: /置顶/ });
  await userEvent.click(pinned);
  expect(pinned.getAttribute("aria-pressed")).toBe("true");
  const box = screen.getByRole("searchbox", { name: "搜索会话 / 项目" }) as HTMLInputElement;
  await userEvent.type(box, "abc");
  expect(box.value).toBe("abc");
  expect(pinned.getAttribute("aria-pressed")).toBe("true");
});

// The scope filters say which sessions the project list shows; the team above
// it is not filtered by them, so they sit under the projects' own heading.
it("the scope filters sit under the projects heading, after the team", async () => {
  const { container } = render(<App hub={hub()} />);
  const group = await screen.findByRole("group", { name: "会话范围" });
  const team = await screen.findByRole("region", { name: "团队" });
  expect(group.closest(".projects-header")).toBeTruthy();
  // One scroll for both: a long team scrolls with the projects, not over them.
  expect(team.closest(".scroll")).toBe(group.closest(".scroll"));
  expect(team.compareDocumentPosition(group) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(container.querySelector(".projects-heading")!.compareDocumentPosition(group) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});
