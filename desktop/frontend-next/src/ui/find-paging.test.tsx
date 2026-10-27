// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Find } from "./Find";
import { useFind } from "./usefind";

vi.mock("../i18n", () => ({ t: (text: string) => text }));
afterEach(cleanup);

function Search({ pending, failed = false }: { pending: boolean; failed?: boolean }) {
  const find = useFind([], 0, true, () => {});
  return <Find find={{ ...find, open: true, query: "earlier message" }} pending={pending} failed={failed} />;
}

it("does not declare no results until all history has been searched", () => {
  const { rerender } = render(<Search pending />);
  expect(screen.getByText("正在查找较早消息…")).toBeTruthy();
  expect(screen.queryByText("无结果")).toBeNull();
  rerender(<Search pending failed />);
  expect(screen.getByText("部分历史尚未读取")).toBeTruthy();
  expect(screen.queryByText("无结果")).toBeNull();
  rerender(<Search pending={false} />);
  expect(screen.getByText("无结果")).toBeTruthy();
});

it("keeps regex and case controls active while earlier history is loading", () => {
  function PagingSearch() {
    const find = useFind([], 1, true, () => {});
    return <Find find={find} pending />;
  }
  render(<PagingSearch />);
  fireEvent.change(screen.getByRole("searchbox"), { target: { value: "earlier.*message" } });
  const regex = screen.getByRole("button", { name: "把查询当作正则表达式" });
  const caseSensitive = screen.getByRole("button", { name: "区分大小写" });
  fireEvent.click(regex);
  fireEvent.click(caseSensitive);
  expect(regex.getAttribute("aria-pressed")).toBe("true");
  expect(caseSensitive.getAttribute("aria-pressed")).toBe("true");
  expect(screen.getByText("正在查找较早消息…")).toBeTruthy();
});
