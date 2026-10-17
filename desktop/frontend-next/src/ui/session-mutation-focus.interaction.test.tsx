// @vitest-environment jsdom
import { useState } from "react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { useSessionMutation } from "./useSessionMutation";

afterEach(cleanup);

function draw() {
  let finish!: () => void;
  const gate = new Promise<void>((resolve) => { finish = resolve; });
  const write = vi.fn(() => gate);
  function Harness() {
    const [rows, setRows] = useState(["first", "next"]);
    const mutation = useSessionMutation(vi.fn());
    return <><input aria-label="Elsewhere" /><div role="tree">
      {rows.map((name) => <div key={name} role="treeitem" data-target={name} tabIndex={0}>
        {name}<button disabled={mutation.pending.has(name)} onClick={(event) => {
          void mutation.run(name, "archive", event.currentTarget.parentElement, async () => {
            await write(); setRows((current) => current.filter((row) => row !== name));
          });
        }}>Archive {name}</button>
      </div>)}
    </div></>;
  }
  render(<Harness />);
  return { finish, write };
}

it("moves focus to a surviving tree row when archiving removes the focused row", async () => {
  const { finish } = draw();
  await userEvent.click(screen.getByRole("button", { name: "Archive first" }));
  await act(async () => { finish(); });
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("treeitem", { name: /next/ })));
});

it("keeps a later input focus when a pending archive removes its old row", async () => {
  const { finish } = draw();
  await userEvent.click(screen.getByRole("button", { name: "Archive first" }));
  const input = screen.getByRole("textbox", { name: "Elsewhere" });
  await userEvent.click(input);
  await act(async () => { finish(); });
  expect(screen.queryByRole("button", { name: "Archive first" })).toBeNull();
  expect(document.activeElement).toBe(input);
});
