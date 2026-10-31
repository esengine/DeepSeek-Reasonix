// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { MockPort } from "../../port/mock";
import type { RuntimeView } from "../../port/hub";
import { teamStore } from "../../state/team";
import { Pane } from "../Pane";

beforeEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function setup() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada", avatar: { glyph: "kitty", color: "cat-3" } });
  const store = teamStore(hub);
  const detach = store.attach();
  await store.refresh();
  const port = new MockPort();
  vi.spyOn(port, "history").mockResolvedValue([
    { role: "user", content: "hello", msgIndex: 0 },
    { role: "assistant", content: "a saved answer", modelRef: "provider/original-model", msgIndex: 1 },
  ]);
  return { team, store, port, detach };
}

function pane(port: MockPort, id: string, seat?: RuntimeView["team"], visible = true) {
  const rt: RuntimeView = { id, base: `/rt/${id}`, root: "/w", name: id, team: seat };
  return <Pane key={id} port={port} rt={rt} title={id} active={visible} visible={visible} sideHost={null} side={false}
    onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
    onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
    theme="dark" dockW={560} dockMax={880} onDockW={() => {}} />;
}

it.each([undefined, "g-1"])("a restored agent reply uses its seat identity, including group seat %s", async (group) => {
  const { team, store, port, detach } = await setup();
  try {
    const { container } = render(pane(port, "ada", { agent: "a-1", group }));
    const reply = () => container.querySelector('.call[data-k="say"]')!;
    await waitFor(() => expect(reply()?.querySelector(".nm")?.textContent).toBe("Ada"));
    expect(reply().querySelector(".team-avatar")?.getAttribute("data-color")).toBe("cat-3");
    expect(reply().querySelector(".sym")).toBeNull();
    expect(reply().querySelector(".src")?.textContent).toBe("original-model");
    await act(async () => store.shown(await team.updateAgent("a-1", { name: "Ada Updated", avatar: { glyph: "star", color: "cat-5" } })));
    expect(reply().querySelector(".nm")?.textContent).toBe("Ada Updated");
    expect(reply().querySelector(".team-avatar")?.getAttribute("data-color")).toBe("cat-5");
    await act(async () => store.shown(await team.updateAgent("a-1", { avatar: { glyph: "star", color: "cat-5", image: "/team/avatars/ada.png" } })));
    const photo = reply().querySelector("img")!;
    expect(photo.getAttribute("src")).toBe("/team/avatars/ada.png");
    fireEvent.error(photo);
    expect(reply().querySelector(".team-avatar svg")).toBeTruthy();
    expect(reply().querySelector(".nm")?.textContent).toBe("Ada Updated");
  } finally { cleanup(); detach(); }
});

it("a removed agent never borrows the current selection's identity", async () => {
  const { team, store, port, detach } = await setup();
  try {
    await act(async () => store.shown(await team.deleteAgent("a-1")));
    const { container } = render(pane(port, "removed", { agent: "a-1" }));
    await waitFor(() => expect(container.querySelector('.call[data-k="say"] .nm')?.textContent).toBe("另一个 Agent"));
    expect(container.querySelector('.call[data-k="say"] .team-avatar')).toBeTruthy();
  } finally { cleanup(); detach(); }
});

it("switching active panes never relabels a background agent or an ordinary reply", async () => {
  const { port, detach } = await setup();
  try {
    const ui = (adaActive: boolean) => <>
      {pane(port, "ada", { agent: "a-1" }, adaActive)}
      {pane(port, "ordinary", undefined, !adaActive)}
    </>;
    const view = render(ui(true));
    const names = () => [...view.container.querySelectorAll('.call[data-k="say"] .nm')].map((n) => n.textContent);
    await waitFor(() => expect(names()).toEqual(["Ada", "reasonix"]));
    view.rerender(ui(false));
    expect(names()).toEqual(["Ada", "reasonix"]);
    const ordinary = view.container.querySelectorAll('.call[data-k="say"]')[1];
    expect(ordinary.querySelector(".sym")).toBeTruthy();
    expect(ordinary.querySelector(".team-avatar")).toBeNull();
  } finally { cleanup(); detach(); }
});
