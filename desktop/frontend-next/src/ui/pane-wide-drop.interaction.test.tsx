// @vitest-environment jsdom
import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import { Composer } from "./Composer";
import { install, useFileDrop } from "./filedrop";
import { PATH_TYPE } from "./filedrag";

beforeAll(install);
afterEach(() => { cleanup(); localStorage.clear(); vi.restoreAllMocks(); });

function transfer(path: string) {
  const data = new Map<string, string>([[PATH_TYPE, path]]);
  return {
    files: [], items: [], effectAllowed: "copy", dropEffect: "none",
    get types() { return [...data.keys()]; },
    setData: (type: string, value: string) => data.set(type, value),
    getData: (type: string) => data.get(type) ?? "",
  };
}

function Pane({ port }: { port: MockPort }) {
  return (
    <section className="pane">
      <div data-testid="transcript">earlier answers</div>
      <Composer port={port} status={null} running={false} onSubmit={vi.fn(async () => true)} onChanged={vi.fn()} onError={vi.fn()} />
    </section>
  );
}

it("takes a file dropped anywhere on the pane, not only on the composer", async () => {
  const port = new MockPort();
  const dropRefs = vi.spyOn(port, "dropRefs").mockImplementation(async (paths) => paths.map((path) => ({ path: "README.md", ref: `@README.md`, image: path.endsWith(".png") })));
  render(<Pane port={port} />);
  const transcript = screen.getByTestId("transcript");
  const dataTransfer = transfer("/workspace/README.md");
  fireEvent.dragOver(transcript, { dataTransfer });
  expect(screen.getByText("松手 → 引用 1 个文件")).toBeTruthy();
  fireEvent.drop(transcript, { dataTransfer });
  await screen.findByRole("button", { name: "移除 README.md" });
  expect(dropRefs).toHaveBeenCalledWith(["/workspace/README.md"]);
});

it("leaves a drop on a nested zone to that zone even when a wider one mounted first", async () => {
  const outer = vi.fn();
  const inner = vi.fn();
  function Nest() {
    const [show, setShow] = useState(false);
    const wide = useFileDrop(outer, () => {});
    const narrow = useFileDrop(inner, () => {});
    return (
      <div data-testid="wide" ref={wide}>
        <button onClick={() => setShow(true)}>open</button>
        {show && <div data-testid="narrow" ref={narrow} />}
      </div>
    );
  }
  render(<Nest />);
  fireEvent.click(screen.getByText("open"));
  const dataTransfer = transfer("/workspace/a.txt");
  act(() => { fireEvent.drop(screen.getByTestId("narrow"), { dataTransfer }); });
  await waitFor(() => expect(inner).toHaveBeenCalledTimes(1));
  expect(outer).not.toHaveBeenCalled();
});
