// @vitest-environment jsdom
import { useRef, useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useModalFocus } from "./useModalFocus";

afterEach(cleanup);

function Dialog({ name, children }: { name: string; children?: React.ReactNode }) {
  const root = useRef<HTMLDivElement>(null);
  useModalFocus(root);
  return (
    <div ref={root} role="dialog" aria-modal="true" aria-label={name} tabIndex={-1}>
      <button>{name} first</button>
      <button>{name} last</button>
      {children}
    </div>
  );
}

function Page() {
  const [open, setOpen] = useState(false);
  const [inner, setInner] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>open</button>
      <button>behind</button>
      {open && (
        <Dialog name="outer">
          <button onClick={() => setInner(true)}>open inner</button>
          <button onClick={() => setOpen(false)}>close outer</button>
          {inner && <Dialog name="inner" />}
        </Dialog>
      )}
    </>
  );
}

it("wraps Tab and Shift+Tab inside the dialog instead of reaching the page behind it", async () => {
  render(<Page />);
  await userEvent.click(screen.getByText("open"));
  const first = screen.getByText("outer first");
  const closeOuter = screen.getByText("close outer");
  first.focus();
  await userEvent.tab({ shift: true });
  expect(document.activeElement).toBe(closeOuter);
  await userEvent.tab();
  expect(document.activeElement).toBe(first);
  for (let i = 0; i < 12; i++) {
    await userEvent.tab();
    expect(document.querySelector('[aria-label="outer"]')!.contains(document.activeElement)).toBe(true);
  }
});

it("gives focus back to the control that opened it", async () => {
  render(<Page />);
  const opener = screen.getByText("open");
  await userEvent.click(opener);
  await userEvent.click(screen.getByText("close outer"));
  expect(document.activeElement).toBe(opener);
});

it("lets only the innermost of two open dialogs hold Tab", async () => {
  render(<Page />);
  await userEvent.click(screen.getByText("open"));
  await userEvent.click(screen.getByText("open inner"));
  screen.getByText("inner last").focus();
  await userEvent.tab();
  expect(document.activeElement).toBe(screen.getByText("inner first"));
  await userEvent.tab({ shift: true });
  expect(document.activeElement).toBe(screen.getByText("inner last"));
});

it.each([
  { visible: false, expected: { preventScroll: true, focusVisible: false } },
  { visible: true, expected: { preventScroll: true } },
])("gives focus back with the ring only if the opener had one (visible=$visible)", async ({ visible, expected }) => {
  const user = userEvent.setup();
  render(<Page />);
  const opener = screen.getByRole("button", { name: "open" });
  const realMatches = opener.matches.bind(opener);
  vi.spyOn(opener, "matches").mockImplementation(((selector: string) => (selector === ":focus-visible" ? visible : realMatches(selector))) as typeof opener.matches);
  const focus = vi.spyOn(opener, "focus");
  await user.click(opener);
  await user.click(screen.getByRole("button", { name: "close outer" }));
  expect(focus).toHaveBeenLastCalledWith(expected);
});
