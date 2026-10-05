// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { useTreeKeys } from "./tree";

afterEach(cleanup);

// A stand-in with the sidebar's shape: a machine holding a project holding two
// sessions, plus a second machine that is collapsed.
function Tree({ onOpen }: { onOpen: (name: string) => void }) {
  const keys = useTreeKeys();
  return (
    <div role="tree" aria-label="树" data-action-keydown="tree.navigate" ref={keys.ref} onKeyDown={keys.onKeyDown}>
      <div role="treeitem" aria-level={1} aria-expanded="true">这台机器</div>
      <div role="treeitem" aria-level={2} aria-expanded="true">项目</div>
      <div role="treeitem" aria-level={3} onClick={() => onOpen("会话一")}>会话一</div>
      <div role="treeitem" aria-level={3} onClick={() => onOpen("会话二")}>会话二</div>
      <div role="treeitem" aria-level={1} aria-expanded="false">远程</div>
    </div>
  );
}

const rows = () => screen.getAllByRole("treeitem");
const focused = () => document.activeElement?.textContent;

describe("walking the sidebar with the keyboard", () => {
  // Every row declared itself a treeitem — which tells a screen reader the
  // arrows work — and not one of them could be reached by any key. What Tab did
  // reach were the buttons inside the rows, so a keyboard could delete a session
  // it had no way to open.
  it("is one stop on the tab path, not one per row", () => {
    render(<Tree onOpen={() => {}} />);
    expect(rows().filter((r) => r.tabIndex === 0)).toHaveLength(1);
  });

  it("moves down and up the rows that are showing", async () => {
    render(<Tree onOpen={() => {}} />);
    rows()[0].focus();
    await userEvent.keyboard("{ArrowDown}");
    expect(focused()).toBe("项目");
    await userEvent.keyboard("{ArrowDown}");
    expect(focused()).toBe("会话一");
    await userEvent.keyboard("{ArrowUp}");
    expect(focused()).toBe("项目");
  });

  // The entry point follows the walk: coming back with Tab lands where the
  // arrows left off, not back at the top.
  it("carries the tab stop with it", async () => {
    render(<Tree onOpen={() => {}} />);
    rows()[0].focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    const stops = rows().filter((r) => r.tabIndex === 0);
    expect(stops).toHaveLength(1);
    expect(stops[0].textContent).toBe("会话一");
  });

  it("opens what the arrows landed on", async () => {
    const opened: string[] = [];
    render(<Tree onOpen={(n) => opened.push(n)} />);
    rows()[0].focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(opened).toEqual(["会话一"]);
  });

  // Right opens a closed branch rather than stepping past it; left closes an
  // open one, and on a leaf goes out to the branch it hangs under.
  it("uses left and right for the branch, not for the list", async () => {
    render(<Tree onOpen={() => {}} />);
    const remote = rows()[4];
    remote.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(remote.getAttribute("aria-expanded")).toBe("false"); // the stub does not really open
    rows()[2].focus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(focused()).toBe("项目");
  });

  it("jumps to the ends", async () => {
    render(<Tree onOpen={() => {}} />);
    rows()[2].focus();
    await userEvent.keyboard("{End}");
    expect(focused()).toBe("远程");
    await userEvent.keyboard("{Home}");
    expect(focused()).toBe("这台机器");
  });
});
