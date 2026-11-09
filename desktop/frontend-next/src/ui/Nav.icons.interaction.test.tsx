// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { Nav } from "./Nav";
import { ICON, type Section } from "./prefsnav";

const props = { at: null, onGo: vi.fn(), onFiles: vi.fn(), onHome: vi.fn(), onRail: vi.fn(), onFeedback: vi.fn(), onNew: vi.fn(), feedbackUnread: 0, shown: true };

afterEach(cleanup);

const sectionMark = (section: Section) => render(<>{ICON[section]}</>).container.querySelector("svg")?.innerHTML;

describe("the icon rail draws the mark of the section it opens", () => {
  it.each([
    ["用量", "usage"],
    ["工具与权限", "tools"],
    ["扩展", "ext"],
    ["记忆", "memory"],
    ["远程", "remote"],
    ["账号", "account"],
    ["设置", "common"],
  ] as const)("%s shows the %s section mark", (label, section) => {
    const { container } = render(<Nav {...props} />);
    const btn = container.querySelector(`button[aria-label="${label}"]`);
    expect(btn).not.toBeNull();
    expect(btn?.querySelector("svg")?.innerHTML).toBe(sectionMark(section));
  });
});


it("routes Files to the explorer without marking it as Storage", () => {
  const { container } = render(<Nav {...props} at="storage" />);
  const files = container.querySelector('button[aria-label="文件"]')!;
  expect(files.hasAttribute("aria-current")).toBe(false);
  expect(files.getAttribute("data-action")).toBe("files.open");
  fireEvent.click(files);
  expect(props.onFiles).toHaveBeenCalledOnce();
  expect(props.onGo).not.toHaveBeenCalled();
});

// Clicking the current section's icon folds the sidebar, so starting a session
// has a button of its own on the rail.
it("starts a new session from its own rail button", () => {
  const onNew = vi.fn();
  const { container } = render(<Nav {...props} onNew={onNew} />);
  const btn = container.querySelector<HTMLButtonElement>('button[data-action="session.new"]')!;
  expect(btn.getAttribute("aria-label")).toBe("新建会话");
  btn.click();
  expect(onNew).toHaveBeenCalledTimes(1);
  expect(props.onRail).not.toHaveBeenCalled();
});
