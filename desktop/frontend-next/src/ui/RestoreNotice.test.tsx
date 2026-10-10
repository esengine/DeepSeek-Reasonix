// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { parse } from "@babel/parser";
import { boot, STORAGE } from "../i18n";
import { ReloadNotice, RestoreNotice } from "./RestoreNotice";
import { walkStack, type Node } from "./roots";
import css from "./RestoreNotice.css?raw";
import pane from "./Pane.tsx?raw";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });
it.each(["zh", "en"])("renders named undo and dismiss buttons in %s", (language) => {
  localStorage.setItem(STORAGE, language); boot();
  render(<RestoreNotice receipt={{ tx: "tx-1", files: 1, working: false, error: "" }} onUndo={vi.fn(async () => {})} onDismiss={vi.fn()} />);
  expect(screen.getByRole("button", { name: language === "zh" ? "撤销这次还原" : "Undo this restore" })).toBeTruthy();
  expect(screen.getAllByRole("button").every((b) => !!b.textContent?.trim())).toBe(true);
});
it("uses wrapping inline layout without mount-triggered motion", () => {
  expect(css).toContain(".restore-notice");
  expect(css).toMatch(/flex-wrap:\s*wrap/);
  expect(css).toMatch(/overflow-wrap:\s*anywhere/);
  expect(css).not.toMatch(/animation|transition|position:\s*(fixed|absolute)/);
});

const style = (selector: string) => {
  const sheet = document.createElement("style");
  sheet.textContent = css;
  document.head.append(sheet);
  const rule = [...sheet.sheet!.cssRules].find((r) => (r as CSSStyleRule).selectorText === selector) as CSSStyleRule | undefined;
  sheet.remove();
  expect(rule, selector).toBeDefined();
  return rule!.style;
};

it("resets native button paint and supplies themed focus and disabled states", () => {
  const button = style(".restore-notice button");
  expect(button.getPropertyValue("border")).toBe("0px");
  expect(button.getPropertyValue("background")).toBe("transparent");
  expect(button.getPropertyValue("font")).toBe("inherit");
  expect(button.getPropertyValue("text-decoration")).toBe("none");
  expect(style(".restore-notice button:focus-visible").getPropertyValue("outline")).toBe("2px solid var(--focus)");
  expect(style(".restore-notice button:disabled").getPropertyValue("opacity")).toBe("0.5");
  expect(style(".restore-notice button:disabled").getPropertyValue("cursor")).toBe("default");
});

it.each(["RestoreNotice", "ReloadNotice"])("reserves composer space for %s outside the runtime-notes ceiling", (component) => {
  const parents: string[] = [];
  walkStack(parse(pane, { sourceType: "module", plugins: ["typescript", "jsx"] }), (node, stack) => {
    if (node.type !== "JSXElement" || ((node.openingElement as Node).name as Node).name !== component) return;
    const parent = stack.slice(0, -1).reverse().find((n) => n.type === "JSXElement");
    const attrs = (parent?.openingElement as Node).attributes as Node[];
    parents.push((attrs.find((a) => (a.name as Node)?.name === "className")?.value as Node)?.value as string);
  });
  expect(parents).toEqual(["compose"]);
});

it("bounds only the error, with zoom-aware scrolling and a keyboard target", () => {
  render(<RestoreNotice receipt={{ tx: "tx-1", files: 1, working: false, error: "A long failure. ".repeat(80) }} onUndo={vi.fn(async () => {})} onDismiss={vi.fn()} />);
  const alert = screen.getByRole("alert");
  expect(alert.tabIndex).toBe(0);
  alert.focus();
  expect(document.activeElement).toBe(alert);
  const error = style('.restore-notice [role="alert"]');
  expect(error.getPropertyValue("max-height")).toBe("min(calc(18vh / var(--zoom, 1)), 6lh)");
  expect(error.getPropertyValue("overflow-y")).toBe("auto");
  expect(error.getPropertyValue("min-width")).toBe("0px");
  expect(style(".restore-notice").getPropertyValue("max-height")).toBe("");
});

it.each(["undo", "refresh"])("keeps actions available after failure and disables both during %s", async (operation) => {
  const undo = vi.fn(async () => {}); const dismiss = vi.fn();
  const props = { onUndo: undo, onDismiss: dismiss, receipt: { tx: "tx-1", files: 1, working: false, error: "Failed" } };
  const view = render(<RestoreNotice {...props} />);
  const [undoButton, dismissButton] = screen.getAllByRole<HTMLButtonElement>("button");
  const user = userEvent.setup();
  await user.click(undoButton); await user.click(dismissButton);
  expect(undo).toHaveBeenCalledExactlyOnceWith("tx-1"); expect(dismiss).toHaveBeenCalledTimes(1);
  view.rerender(<RestoreNotice {...props} refreshing={operation === "refresh"} receipt={{ ...props.receipt, working: operation === "undo" }} />);
  expect(undoButton.disabled && dismissButton.disabled).toBe(true);
  await user.click(undoButton); await user.click(dismissButton);
  expect(undo).toHaveBeenCalledTimes(1); expect(dismiss).toHaveBeenCalledTimes(1);
});

it("lets a failed refresh be retried with a named button and keyboard-readable error", async () => {
  const reload = vi.fn(async () => {});
  const view = render(<ReloadNotice failure={{ error: "Refresh failed", working: false }} onReload={reload} />);
  const button = screen.getByRole<HTMLButtonElement>("button", { name: "重试刷新会话" });
  expect(screen.getByRole("alert").tabIndex).toBe(0);
  const user = userEvent.setup(); await user.click(button);
  expect(reload).toHaveBeenCalledTimes(1);
  view.rerender(<ReloadNotice failure={{ error: "Refresh failed", working: true }} onReload={reload} />);
  expect(button.disabled).toBe(true); await user.click(button);
  expect(reload).toHaveBeenCalledTimes(1);
});
