// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { t } from "../i18n";
import { CodeBlock } from "./CodeBlock";
import { CopyButton, copyText } from "./CopyButton";

afterEach(cleanup);

// A copy button names what it copies wherever it says anything: on hover, to a
// screen reader, and in its visible text when it has some.
describe("a copy button", () => {
  it("on a code block, offers to copy the code rather than the response", () => {
    render(<CodeBlock lang="go" source={"x := 1\n"}><code>x := 1</code></CodeBlock>);
    const button = screen.getByRole("button", { name: t("复制这段代码") });
    expect(button.getAttribute("title")).toBe(t("复制这段代码"));
  });

  it("with a label and visible text, says that label instead of the response's", () => {
    render(<CopyButton text="/home/me/.reasonix/config.toml" label={t("复制路径")} />);
    const button = screen.getByRole("button", { name: t("复制路径") });
    expect(button.textContent).toBe(t("复制路径"));
    expect(button.getAttribute("title")).toBe(t("复制路径"));
  });

  it("without a label, still offers to copy the response", () => {
    render(<CopyButton text="an answer" iconOnly />);
    expect(screen.getByRole("button").getAttribute("title")).toBe(t("复制回复"));
  });
});

describe("copying without the Clipboard API", () => {
  let clipboard: PropertyDescriptor | undefined;
  let command: PropertyDescriptor | undefined;

  beforeEach(() => {
    clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    command = Object.getOwnPropertyDescriptor(document, "execCommand");
    const select = HTMLTextAreaElement.prototype.select;
    // Browsers focus the selected textarea; jsdom implements selection alone.
    vi.spyOn(HTMLTextAreaElement.prototype, "select").mockImplementation(function (this: HTMLTextAreaElement) {
      this.focus();
      select.call(this);
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
    else Reflect.deleteProperty(navigator, "clipboard");
    if (command) Object.defineProperty(document, "execCommand", command);
    else Reflect.deleteProperty(document, "execCommand");
    document.querySelectorAll("textarea[readonly]").forEach((area) => area.remove());
  });

  const legacy = (exec: () => boolean) => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    const fn = vi.fn(exec);
    Object.defineProperty(document, "execCommand", { configurable: true, value: fn });
    return fn;
  };

  it.each([true, false])("restores button focus without scrolling after copy returns %s", async (ok) => {
    render(<CopyButton text="第一行\n  /tool:run" label="Copy invocation" />);
    const button = screen.getByRole("button");
    button.focus();
    const focus = vi.spyOn(button, "focus");
    const exec = legacy(() => {
      const selected = document.activeElement as HTMLTextAreaElement;
      expect(selected.tagName).toBe("TEXTAREA");
      expect(selected.value).toBe("第一行\n  /tool:run");
      expect(selected.readOnly).toBe(true);
      return ok;
    });
    const result = copyText("第一行\n  /tool:run");
    if (ok) await expect(result).resolves.toBeUndefined();
    else await expect(result).rejects.toThrow("copy rejected");
    expect(exec).toHaveBeenCalledExactlyOnceWith("copy");
    expect(document.querySelector("textarea")).toBeNull();
    expect(document.activeElement).toBe(button);
    expect(focus).toHaveBeenCalledExactlyOnceWith({ preventScroll: true });
  });

  it("cleans up and restores focus when the copy command throws", async () => {
    render(<button>Copy path</button>);
    const button = screen.getByRole("button");
    button.focus();
    const failure = new DOMException("clipboard unavailable", "InvalidStateError");
    legacy(() => { throw failure; });
    await expect(copyText("/project")).rejects.toBe(failure);
    expect(document.querySelector("textarea")).toBeNull();
    expect(document.activeElement).toBe(button);
  });

  it("restores the focused editor and its caret selection", async () => {
    render(<textarea aria-label="Editor" defaultValue="abcdef" />);
    const editor = screen.getByRole("textbox") as HTMLTextAreaElement;
    editor.focus();
    editor.setSelectionRange(1, 4, "backward");
    legacy(() => true);
    await copyText("a shareable snippet");
    expect(document.activeElement).toBe(editor);
    expect([editor.selectionStart, editor.selectionEnd, editor.selectionDirection]).toEqual([1, 4, "backward"]);
    expect(editor.value).toBe("abcdef");
    expect(document.querySelectorAll("textarea")).toHaveLength(1);
  });

  it("keeps keyboard retry and the next action reachable after refusal", async () => {
    const user = userEvent.setup();
    const exec = legacy(() => false);
    render(<><CopyButton text="/skill" label="Copy invocation" showFeedback /><button>Next action</button></>);
    await user.tab();
    const copy = screen.getByRole("button", { name: "Copy invocation" });
    await user.keyboard("{Enter}");
    await waitFor(() => expect(copy.getAttribute("data-state")).toBe("failed"));
    expect(copy.title).toBe(t("复制失败，请重试"));
    expect(document.activeElement).toBe(copy);
    exec.mockReturnValue(true);
    await user.keyboard("{Enter}");
    await waitFor(() => expect(copy.getAttribute("data-state")).toBe("done"));
    expect(copy.title).toBe(t("已复制"));
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Next action" }));
    expect(exec).toHaveBeenCalledTimes(2);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("does not focus a previous control removed during copying", async () => {
    const view = render(<button>Copy</button>);
    const button = screen.getByRole("button");
    button.focus();
    legacy(() => { view.unmount(); return true; });
    await expect(copyText("text")).resolves.toBeUndefined();
    expect(document.activeElement).toBe(document.body);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("also removes the carrier when there was no focused control", async () => {
    legacy(() => true);
    await expect(copyText("")).resolves.toBeUndefined();
    expect(document.activeElement).toBe(document.body);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it.each([true, false])("uses the modern Clipboard API exclusively when it succeeds = %s", async (ok) => {
    render(<button>Copy</button>);
    const button = screen.getByRole("button");
    button.focus();
    const exec = legacy(() => true);
    const failure = new DOMException("denied", "NotAllowedError");
    const writeText = vi.fn(() => ok ? Promise.resolve() : Promise.reject(failure));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const result = copyText("line one\nline two");
    if (ok) await expect(result).resolves.toBeUndefined();
    else await expect(result).rejects.toBe(failure);
    expect(writeText).toHaveBeenCalledExactlyOnceWith("line one\nline two");
    expect(exec).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(button);
    expect(document.querySelector("textarea")).toBeNull();
  });
});
