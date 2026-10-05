// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Item } from "../../state/session";
import { ElicitCard } from "./ElicitCard";

const { open } = vi.hoisted(() => ({ open: vi.fn() }));
vi.mock("../../port/host", () => ({ host: () => ({ openExternal: open }) }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const request = (extra: Partial<Extract<Item, { t: "ask" }>> = {}): Extract<Item, { t: "ask" }> => ({
  t: "ask", id: "row", ask: { id: "ask", origin: { kind: "mcp", source: "external", message: "Finish <b>here</b> at https://other.invalid", url: "http://xn--bcher-kva.example/connect?state=opaque" }, questions: [{ id: "mcp.url", prompt: "Complete an external interaction", options: [] }] }, ...extra,
});

describe("external MCP interaction", () => {
  it("shows plain text and full URL without opening or answering on mount", async () => {
    const answer = vi.fn().mockResolvedValue(undefined);
    render(<ElicitCard item={request()} onAnswer={answer} />);
    expect(screen.getByText("http://xn--bcher-kva.example/connect?state=opaque")).toBeTruthy();
    expect(screen.getByText("xn--bcher-kva.example").tagName).toBe("B");
    expect(screen.getByText("Finish <b>here</b> at https://other.invalid")).toBeTruthy();
    expect(screen.getByRole("note")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(open).not.toHaveBeenCalled();
    expect(answer).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "打开浏览器" }));
    expect(open).toHaveBeenCalledWith("http://xn--bcher-kva.example/connect?state=opaque");
    expect(answer).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "已完成，继续" }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "mcp.url", selected: ["accept"] }]);
  });

  it.each([
    ["https://0x7f.1/connect", "127.0.0.1"],
    ["https://[::1]/connect", "[::1]"],
    ["https://[fd00::1]/connect", "[fd00::1]"],
  ])("shows the normalized local target for %s without prefetching", (url, urlHost) => {
    const item = request();
    item.ask.origin = { kind: "mcp", source: "external", url, urlHost, urlLocal: true };
    render(<ElicitCard item={item} onAnswer={vi.fn()} />);
    expect(screen.getByText(urlHost).tagName).toBe("B");
    expect(screen.getByRole("note").textContent).toContain("本机或私有网络");
    expect(open).not.toHaveBeenCalled();
  });

  it.each([["拒绝提供", "decline"], ["取消", "cancel"]])("sends only the %s action", async (label, action) => {
    const answer = vi.fn().mockResolvedValue(undefined);
    render(<ElicitCard item={request()} onAnswer={answer} />);
    await userEvent.click(screen.getByRole("button", { name: label }));
    expect(answer).toHaveBeenCalledWith("row", "ask", [{ questionId: "mcp.url", selected: [action] }]);
    expect(open).not.toHaveBeenCalled();
  });

  it("keeps an opener failure pending and permits a retry", async () => {
    open.mockRejectedValueOnce(new Error("secret-url"));
    render(<ElicitCard item={request()} onAnswer={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "打开浏览器" }));
    expect(screen.getByRole("alert").textContent).not.toContain("secret-url");
    await userEvent.click(screen.getByRole("button", { name: "打开浏览器" }));
    expect(open).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each([{ answered: [["accept"]] }, { answered: [["cancel"]] }, { answered: [[]] }])("cannot open or answer a settled or expired card", ({ answered }) => {
    render(<ElicitCard item={request({ answered, answeredElsewhere: true })} onAnswer={vi.fn()} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(open).not.toHaveBeenCalled();
  });
});
