// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { useEscape } from "./dismiss";

afterEach(cleanup);

function Inner({ onClose }: { onClose: () => void }) {
  useEscape(true, onClose);
  return <i data-testid="inner" />;
}

function Outer({ onOuter, onInner }: { onOuter: () => void; onInner: () => void }) {
  const [viewing, setViewing] = useState(false);
  useEscape(true, onOuter);
  return (
    <div>
      <button onClick={() => setViewing(true)}>open</button>
      {viewing && <Inner onClose={() => { onInner(); setViewing(false); }} />}
    </div>
  );
}

it("answers Escape in the innermost open layer only", () => {
  const outer = vi.fn();
  const inner = vi.fn();
  render(<Outer onOuter={outer} onInner={inner} />);
  fireEvent.click(screen.getByText("open"));
  fireEvent.keyDown(window, { key: "Escape" });
  expect(inner).toHaveBeenCalledTimes(1);
  expect(outer).not.toHaveBeenCalled();
  fireEvent.keyDown(window, { key: "Escape" });
  expect(outer).toHaveBeenCalledTimes(1);
});

it("keeps the outer layer below the inner one when the outer re-renders with a new close handler", () => {
  const outer = vi.fn();
  const inner = vi.fn();
  const view = render(<Outer onOuter={() => outer()} onInner={inner} />);
  fireEvent.click(screen.getByText("open"));
  view.rerender(<Outer onOuter={() => outer()} onInner={inner} />);
  fireEvent.keyDown(window, { key: "Escape" });
  expect(inner).toHaveBeenCalledTimes(1);
  expect(outer).not.toHaveBeenCalled();
});
