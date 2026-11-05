// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Workspaces } from "./Workspaces";
import { LegacySessionsSection } from "./LegacySessionsSection";
import { setHidesLegacySessions } from "../state/prefs";
import type { TreeSession, TreeWorkspace } from "../port/hub";

beforeEach(() => setHidesLegacySessions(false));
afterEach(() => {
  cleanup();
  setHidesLegacySessions(false);
});

const OLD = "/w/.reasonix/sessions/old.jsonl";
const NEW = "/w/.reasonix/sessions/new.jsonl";

function draw(opts: { search?: string; runtimeId?: string } = {}) {
  const old: TreeSession = { path: OLD, name: "old", title: "an old one", legacy: true, runtimeId: opts.runtimeId };
  const fresh: TreeSession = { path: NEW, name: "new", title: "a new one" };
  const tree: TreeWorkspace[] = [{ root: "/w", name: "w", sessions: [old, fresh] }];
  render(<Workspaces
    hub={{} as never} tree={tree} treeRead
    runtimes={opts.runtimeId ? [{ id: opts.runtimeId, base: "", root: "/w", name: "w", sessionPath: OLD }] : []}
    active="" folded={new Set()} onFold={() => {}} reload={vi.fn()}
    onOpen={vi.fn()} onFocus={vi.fn()} onClose={vi.fn()}
    liveIds={(ids) => (opts.runtimeId ? ids : [])} runs={{}} scope="all"
    onArchive={vi.fn()} onRename={() => {}} onError={vi.fn()}
    adder={{ add: () => {}, close: () => {}, at: null } as never}
  />);
}

it("lists sessions written by 1.x by default", () => {
  draw();
  expect(screen.getByText("an old one")).toBeTruthy();
  expect(screen.getByText("a new one")).toBeTruthy();
});

it("keeps them out of the list once hidden, and leaves the others", () => {
  setHidesLegacySessions(true);
  draw();
  expect(screen.queryByText("an old one")).toBeNull();
  expect(screen.getByText("a new one")).toBeTruthy();
});

it("still shows one that is running", () => {
  setHidesLegacySessions(true);
  draw({ runtimeId: "r1" });
  expect(screen.getByText("an old one")).toBeTruthy();
});

it("is a choice the settings section makes and remembers", async () => {
  render(<LegacySessionsSection />);
  expect(screen.getByRole("radio", { name: "显示" }).getAttribute("aria-checked")).toBe("true");
  await userEvent.click(screen.getByRole("radio", { name: "隐藏" }));
  expect(screen.getByRole("radio", { name: "隐藏" }).getAttribute("aria-checked")).toBe("true");
  expect(localStorage.getItem("rx-hide-legacy-sessions")).toBe("on");
});
