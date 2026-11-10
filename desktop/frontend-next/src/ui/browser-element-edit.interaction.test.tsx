// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import "./testkit";
import { host, type BrowserControl, type BrowserPick, type BrowserPickLabels } from "../port/host";
import type { BrowserTab } from "../port/port";
import { AgentBrowserPanel } from "./BrowserPanel";
import type { PageEditor } from "./pageedit";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const file: BrowserTab = { id: "b1", target: "v1", title: "Demo", url: "file:///work/site/a.html", active: true };
const web: BrowserTab = { id: "b2", target: "v2", title: "Web", url: "https://example.com/", active: false };
const picked: BrowserPick = {
  cancelled: false,
  element: { selector: "#note", tagName: "p", text: "hello", style: { color: "#111111", backgroundColor: "#FFFFFF", fontSize: "16px" } },
};

let pick: Mock<(target: string, labels: BrowserPickLabels) => Promise<BrowserPick>>;
let cancel: Mock<(target: string) => void>;
let control: Mock<(target: string, action: BrowserControl) => void>;

beforeEach(() => {
  const shell = host();
  vi.spyOn(shell, "drawsBrowserViews").mockReturnValue(true);
  vi.spyOn(shell, "picksBrowserElements").mockReturnValue(true);
  vi.spyOn(shell, "showBrowserView").mockImplementation(() => {});
  vi.spyOn(shell, "hideBrowserView").mockImplementation(() => {});
  pick = vi.fn(async () => picked);
  cancel = vi.fn();
  control = vi.fn();
  vi.spyOn(shell, "pickBrowserElement").mockImplementation(pick);
  vi.spyOn(shell, "cancelBrowserPick").mockImplementation(cancel);
  vi.spyOn(shell, "controlBrowserView").mockImplementation(control);
});

const pickButton = () => screen.getByRole("button", { name: "点选页面元素" });

describe("editing a picked element of a local page", () => {
  it("offers the picker only where the shell can pick", () => {
    vi.spyOn(host(), "picksBrowserElements").mockReturnValue(false);
    render(<AgentBrowserPanel tabs={[file]} shown />);
    expect(screen.queryByRole("button", { name: "点选页面元素" })).toBeNull();
  });

  it("saves the changed fields only, and reloads the page that was edited", async () => {
    const editor: PageEditor = vi.fn(async () => ({ saved: true as const, path: "a.html" }));
    render(<AgentBrowserPanel tabs={[file]} shown editPage={editor} />);
    fireEvent.click(pickButton());
    expect(pick).toHaveBeenCalledWith("v1", expect.objectContaining({ color: expect.any(String) }));
    const form = await screen.findByRole("form", { name: "修改所选元素" });
    fireEvent.change(screen.getByLabelText("文字"), { target: { value: "bye" } });
    fireEvent.change(screen.getByLabelText("文字颜色"), { target: { value: "#ff0000" } });
    fireEvent.submit(form);
    await waitFor(() => expect(editor).toHaveBeenCalledTimes(1));
    expect(editor).toHaveBeenCalledWith("/work/site/a.html", { selector: "#note", tagName: "p" }, [
      { kind: "text", before: "hello", after: "bye" },
      { kind: "style", property: "color", value: "#ff0000" },
    ]);
    expect(await screen.findByText("已保存到 a.html")).toBeTruthy();
    expect(control).toHaveBeenCalledWith("v1", "reload");
  });

  it("measures a second save from what the first one wrote", async () => {
    const editor: PageEditor = vi.fn(async () => ({ saved: true as const, path: "a.html" }));
    render(<AgentBrowserPanel tabs={[file]} shown editPage={editor} />);
    fireEvent.click(pickButton());
    const form = await screen.findByRole("form", { name: "修改所选元素" });
    fireEvent.change(screen.getByLabelText("文字"), { target: { value: "bye" } });
    fireEvent.submit(form);
    await screen.findByText("已保存到 a.html");
    fireEvent.change(screen.getByLabelText("文字"), { target: { value: "again" } });
    fireEvent.submit(form);
    await waitFor(() => expect(editor).toHaveBeenCalledTimes(2));
    expect((editor as ReturnType<typeof vi.fn>).mock.calls[1][2]).toEqual([{ kind: "text", before: "bye", after: "again" }]);
  });

  it("says why a save was refused and leaves the page alone", async () => {
    const editor: PageEditor = vi.fn(async () => ({ saved: false as const, code: "mismatch" as const }));
    render(<AgentBrowserPanel tabs={[file]} shown editPage={editor} />);
    fireEvent.click(pickButton());
    const form = await screen.findByRole("form", { name: "修改所选元素" });
    fireEvent.change(screen.getByLabelText("文字"), { target: { value: "bye" } });
    fireEvent.submit(form);
    expect((await screen.findByRole("alert")).textContent).toBe("文件里的文字和页面上显示的不一致，未做修改");
    expect(control).not.toHaveBeenCalled();
  });

  it("does not call the editor when nothing was changed", async () => {
    const editor: PageEditor = vi.fn();
    render(<AgentBrowserPanel tabs={[file]} shown editPage={editor} />);
    fireEvent.click(pickButton());
    fireEvent.submit(await screen.findByRole("form", { name: "修改所选元素" }));
    expect((await screen.findByRole("alert")).textContent).toBe("没有改动");
    expect(editor).not.toHaveBeenCalled();
  });

  it("shows a page that is not a local file but will not save to it", async () => {
    const editor: PageEditor = vi.fn();
    render(<AgentBrowserPanel tabs={[web]} shown editPage={editor} />);
    fireEvent.click(pickButton());
    await screen.findByRole("form", { name: "修改所选元素" });
    expect(screen.getByText("只有本地网页文件可以在这里修改并保存")).toBeTruthy();
    expect((screen.getByRole("button", { name: "保存到文件" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("reports why a pick ended without an element, and nothing for Escape", async () => {
    pick.mockResolvedValueOnce({ cancelled: true, reason: "navigation" });
    render(<AgentBrowserPanel tabs={[file]} shown />);
    fireEvent.click(pickButton());
    expect(await screen.findByText("页面已跳转或重新加载，点选已取消")).toBeTruthy();
    pick.mockResolvedValueOnce({ cancelled: true, reason: "escape" });
    fireEvent.click(pickButton());
    await waitFor(() => expect(screen.queryByText("页面已跳转或重新加载，点选已取消")).toBeNull());
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("puts the picker away when the button is pressed again", async () => {
    let end: (answer: BrowserPick) => void = () => {};
    pick.mockImplementationOnce(() => new Promise<BrowserPick>((done) => { end = done; }));
    render(<AgentBrowserPanel tabs={[file]} shown />);
    fireEvent.click(pickButton());
    expect(await screen.findByText("在页面上点一个元素，按 Esc 取消")).toBeTruthy();
    fireEvent.click(pickButton());
    expect(cancel).toHaveBeenCalledWith("v1");
    await act(async () => end({ cancelled: true, reason: "dismissed" }));
    expect(screen.queryByText("在页面上点一个元素，按 Esc 取消")).toBeNull();
  });

  it("drops the picked element when another page is shown", async () => {
    render(<AgentBrowserPanel tabs={[file, web]} shown />);
    fireEvent.click(pickButton());
    await screen.findByRole("form", { name: "修改所选元素" });
    fireEvent.click(screen.getByRole("tab", { name: "Web" }));
    await waitFor(() => expect(screen.queryByRole("form", { name: "修改所选元素" })).toBeNull());
  });
});
