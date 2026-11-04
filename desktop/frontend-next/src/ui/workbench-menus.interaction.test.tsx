// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { BrowserTabMenu } from "./BrowserTabMenu";
import { WorkbenchFileMenu } from "./WorkbenchFileMenu";

afterEach(cleanup);

describe("the workbench menus, as the WB-Menus board draws them", () => {
  it("names the file and its folder at the top, and leads with adding it to the conversation", () => {
    const anchor = document.createElement("button");
    document.body.append(anchor);
    render(<WorkbenchFileMenu menu={{ path: "README.md", folder: false, x: 0, y: 0, anchor }} root="/work" revealable platform="darwin"
      onReveal={vi.fn()} onAddToChat={vi.fn()} onClose={vi.fn()} onOpen={vi.fn()} />);
    expect(document.querySelector(".tabmenu-head")!.textContent).toBe("README.md根目录");
    expect(screen.getAllByRole("menuitem").map((b) => b.getAttribute("data-action"))).toEqual([
      "workbench.add-to-chat", "workbench.copy-ref", "workbench.copy-path", "workbench.reveal", "workbench.open-file",
    ]);
    expect(screen.getByRole("menuitem", { name: "复制引用" }).querySelector("kbd")!.textContent).toBe("@README.md");
  });

  it("says which browser tab it is for and how many each choice closes", () => {
    const anchor = document.createElement("button");
    document.body.append(anchor);
    render(<BrowserTabMenu menu={{ surface: { kind: "manual", id: "m1" }, x: 0, y: 0, anchor }} others={2} right={1} all={3} label="localhost:5173" onDismiss={vi.fn()} onClose={vi.fn()} />);
    expect(document.querySelector(".tabmenu-head")!.textContent).toBe("浏览器标签 · localhost:5173");
    expect(screen.getByRole("menuitem", { name: "关闭其他浏览器标签" }).querySelector("kbd")!.textContent).toBe("2");
    expect(screen.getByRole("menuitem", { name: "关闭全部浏览器标签" }).querySelector("kbd")!.textContent).toBe("3");
  });
});
