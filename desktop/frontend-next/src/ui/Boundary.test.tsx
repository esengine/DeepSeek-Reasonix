// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Boundary } from "./Boundary";

afterEach(cleanup);

function Flaky({ text }: { text: string }) {
  if (text.includes("bad")) throw new Error("renderer failed");
  return <p className="ok">{text}</p>;
}

// A throw on one state of a streamed answer falls back for that state only:
// the next text gets another try, so the finished answer renders normally.
describe("a boundary around rendered model output", () => {
  it("tries again when what it renders changes", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const view = (text: string) => (
      <Boundary retryKey={text} fallback={<div className="fb">{text}</div>}>
        <Flaky text={text} />
      </Boundary>
    );
    const { container, rerender } = render(view("half a bad chunk"));
    expect(container.querySelector(".fb")).not.toBeNull();
    rerender(view("the whole answer"));
    expect(container.querySelector(".ok")?.textContent).toBe("the whole answer");
  });

  it("stays on its fallback while nothing changes", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { container, rerender } = render(<Boundary retryKey="k" fallback={<div className="fb" />}><Flaky text="bad" /></Boundary>);
    rerender(<Boundary retryKey="k" fallback={<div className="fb" />}><Flaky text="bad" /></Boundary>);
    expect(container.querySelector(".fb")).not.toBeNull();
  });
});
