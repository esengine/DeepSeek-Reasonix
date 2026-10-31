// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { MockHub } from "../../port/mock_hub";
import type { RuntimeView } from "../../port/hub";
import { useSessionSwitch } from "../sessionswitch";
import { TeamRail } from "./TeamRail";
import { useAdoptPane } from "./useAdoptPane";

afterEach(cleanup);

it("a delayed team open cannot replace a newer team or ordinary selection", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada", role: "Writer" });
  await team.createAgent({ name: "Ben", role: "Coder" });
  const pending = new Map<string, (rt: RuntimeView) => void>();
  vi.spyOn(team, "openAgent").mockImplementation((id) => new Promise((resolve) => pending.set(id, resolve)));
  function Window() {
    const [runtimes, setRuntimes] = useState<RuntimeView[]>([]);
    const [active, setActive] = useState("");
    const sw = useSessionSwitch();
    const adopt = useAdoptPane(setRuntimes, setActive, sw);
    return <>
      <TeamRail hub={hub} active={active} onAdoptPane={adopt} />
      <button onClick={() => { sw.cancel(); setActive("ordinary"); }}>ordinary</button>
      <output aria-label="active">{active}</output>
      <output aria-label="opened">{runtimes.map((r) => r.id).join(",")}</output>
    </>;
  }
  render(<Window />);
  await userEvent.click(await screen.findByRole("button", { name: "Ada Writer" }));
  await userEvent.click(screen.getByRole("button", { name: "Ben Coder" }));
  await act(async () => pending.get("a-2")!({ id: "ben", name: "Ben", root: "/ben", base: "/rt/ben" }));
  await waitFor(() => expect(screen.getByLabelText("active").textContent).toBe("ben"));
  await userEvent.click(screen.getByRole("button", { name: "ordinary" }));
  await act(async () => pending.get("a-1")!({ id: "ada", name: "Ada", root: "/ada", base: "/rt/ada" }));
  expect(screen.getByLabelText("active").textContent).toBe("ordinary");
  expect(screen.getByLabelText("opened").textContent).toBe("ben,ada");
});
