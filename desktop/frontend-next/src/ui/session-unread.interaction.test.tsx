// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";
import type { AgentPort } from "../port/port";
import type { RuntimeView, TreeWorkspace } from "../port/hub";
import type { WireEvent } from "../port/wire";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const MAIN = "/sessions/mock.jsonl";
const RELEASE = "/sessions/older.jsonl";
const SITE = "/sessions/site.jsonl";
const DOCS = "/sessions/docs.jsonl";

interface Kernel {
  hub: MockHub;
  unread: Set<string>;
  viewed: string[];
  field: { present: boolean };
  gate: { hold: boolean; fail: boolean; release: () => void };
  emit: (path: string, ev: object) => Promise<void>;
  port: (path: string) => AgentPort & { markSessionViewed: ReturnType<typeof vi.fn> };
}

function kernel(unread: string[], present = true): Kernel {
  const hub = new MockHub();
  const unreadSet = new Set(unread);
  const viewed: string[] = [];
  const field = { present };
  const listeners = new Map<string, (ev: WireEvent) => void>();
  const ports = new Map<string, AgentPort & { markSessionViewed: ReturnType<typeof vi.fn> }>();
  let pending: (() => void)[] = [];
  const gate = {
    hold: false,
    fail: false,
    release: () => {
      pending.forEach((go) => go());
      pending = [];
    },
  };
  const open = hub.open.bind(hub);
  hub.open = (req) => open(req);
  const build = hub.portFor.bind(hub);
  hub.portFor = (rt: RuntimeView) => {
    const port = build(rt) as AgentPort & { markSessionViewed: ReturnType<typeof vi.fn> };
    if (!ports.has(rt.id)) {
      ports.set(rt.id, port);
      port.providerSetup = async () => null;
      port.welcomeSeen = async () => true;
      const subscribe = port.subscribe.bind(port);
      vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap, bootstrap) => {
        listeners.set(rt.sessionPath ?? "", onEvent);
        return subscribe(onEvent, onGap, bootstrap);
      });
      port.markSessionViewed = vi.fn(
        () =>
          new Promise<void>((resolve, reject) => {
            const done = () => {
              if (gate.fail) return reject(new Error("refused"));
              unreadSet.delete(rt.sessionPath ?? "");
              viewed.push(rt.sessionPath ?? "");
              resolve();
            };
            if (gate.hold) pending.push(done);
            else done();
          }),
      );
    }
    return port;
  };
  hub.tree = () => {
    const open = new Map((hub as unknown as { views: RuntimeView[] }).views.filter((v) => v.sessionPath).map((v) => [v.sessionPath!, v.id]));
    const row = (path: string, name: string, title: string) => ({
      path,
      name,
      title,
      turns: 3,
      runtimeId: open.get(path),
      ...(field.present && unreadSet.has(path) ? { unread: true } : {}),
    });
    return Promise.resolve<TreeWorkspace[]>([
      {
        root: "~/projects/acme",
        name: "acme",
        remembered: true,
        open: true,
        sessions: [row(MAIN, "mock", "Billing refactor"), row(RELEASE, "older", "Release notes"), row(DOCS, "docs", "Docs sweep")],
      },
      { root: "~/projects/example", name: "example", remembered: true, sessions: [row(SITE, "site", "Landing page")] },
    ]);
  };
  return {
    hub,
    unread: unreadSet,
    viewed,
    field,
    gate,
    emit: async (path, ev) => {
      await waitFor(() => expect(listeners.has(path)).toBe(true));
      act(() => listeners.get(path)!(ev as WireEvent));
    },
    port: (path) => {
      const id = [...ports.keys()].find((k) => (hub as unknown as { views: RuntimeView[] }).views.find((v) => v.id === k)?.sessionPath === path);
      return ports.get(id!)!;
    },
  };
}

const row = (name: RegExp) => screen.findByRole("treeitem", { name });
const dotIn = (el: HTMLElement) => within(el).queryByRole("img", { name: "未读" });
const focused = (on: boolean) => vi.spyOn(document, "hasFocus").mockReturnValue(on);

beforeEach(() => {
  focused(true);
});

describe("an unread session is marked on its row", () => {
  it("shows a dot with an accessible name and a bold title only on the unread row", async () => {
    const k = kernel([RELEASE]);
    render(<App hub={k.hub} />);
    const unread = await row(/Release notes/);
    await waitFor(() => expect(dotIn(unread)).toBeTruthy());
    expect(unread.hasAttribute("data-unread")).toBe(true);
    expect(dotIn(unread)!.compareDocumentPosition(unread.querySelector(".sesstitle")!) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    const read = await row(/Docs sweep/);
    expect(dotIn(read)).toBeNull();
    expect(read.hasAttribute("data-unread")).toBe(false);
  });

  it("treats a kernel that sends no unread field as everything read, without an error", async () => {
    const k = kernel([RELEASE], false);
    render(<App hub={k.hub} />);
    const r = await row(/Release notes/);
    expect(dotIn(r)).toBeNull();
    await userEvent.click(r);
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    expect(k.port(RELEASE).markSessionViewed).not.toHaveBeenCalled();
    expect(document.querySelector('[role="alert"]')).toBeNull();
  });

  it("leaves the finished bar a state of its own: unread does not change data-run", async () => {
    const k = kernel([RELEASE]);
    render(<App hub={k.hub} />);
    const unread = await row(/Release notes/);
    const read = await row(/Docs sweep/);
    expect(unread.getAttribute("data-run")).toBe(read.getAttribute("data-run"));
  });
});

describe("opening a session clears its mark", () => {
  it("calls the pane's viewed endpoint and clears the dot before the kernel answers", async () => {
    const k = kernel([RELEASE]);
    k.gate.hold = true;
    render(<App hub={k.hub} />);
    const target = await row(/Release notes/);
    await waitFor(() => expect(dotIn(target)).toBeTruthy());

    await userEvent.click(target);

    await waitFor(() => expect(k.port(RELEASE)?.markSessionViewed).toHaveBeenCalledTimes(1));
    expect(dotIn(await row(/Release notes/))).toBeNull();
    act(() => k.gate.release());
    await waitFor(() => expect(k.unread.has(RELEASE)).toBe(false));
    expect(dotIn(await row(/Release notes/))).toBeNull();
  });

  it("puts the dot back when the kernel refuses, and says nothing", async () => {
    const k = kernel([RELEASE]);
    k.gate.fail = true;
    render(<App hub={k.hub} />);
    const target = await row(/Release notes/);
    await waitFor(() => expect(dotIn(target)).toBeTruthy());

    await userEvent.click(target);

    await waitFor(() => expect(k.port(RELEASE)?.markSessionViewed).toHaveBeenCalled());
    await waitFor(() => expect(dotIn(screen.getByRole("treeitem", { name: /Release notes/ }))).toBeTruthy());
    expect(document.querySelector('[role="alert"]')).toBeNull();
  });

  it("clears a session that is already the active pane when its row is clicked again", async () => {
    const k = kernel([]);
    render(<App hub={k.hub} />);
    const current = await row(/Billing refactor/);
    await waitFor(() => expect(current.getAttribute("aria-selected")).toBe("true"));
    focused(false);
    k.unread.add(MAIN);
    await k.emit(MAIN, { kind: "turn_started" });
    await k.emit(MAIN, { kind: "turn_done" });
    await waitFor(() => expect(dotIn(screen.getByRole("treeitem", { name: /Billing refactor/ }))).toBeTruthy());
    expect(k.port(MAIN).markSessionViewed).not.toHaveBeenCalled();
    focused(true);
    await userEvent.click(screen.getByRole("treeitem", { name: /Billing refactor/ }));
    await waitFor(() => expect(k.port(MAIN).markSessionViewed).toHaveBeenCalledTimes(1));
    expect(dotIn(screen.getByRole("treeitem", { name: /Billing refactor/ }))).toBeNull();
  });
});

describe("a turn ending while its pane is in front", () => {
  it("marks the session viewed again when the window has focus", async () => {
    const k = kernel([]);
    render(<App hub={k.hub} />);
    await waitFor(() => expect(k.port(MAIN)).toBeTruthy());
    await k.emit(MAIN, { kind: "turn_started" });
    await k.emit(MAIN, { kind: "turn_done" });
    await waitFor(() => expect(k.port(MAIN).markSessionViewed).toHaveBeenCalledTimes(1));
  });

  it("does not mark it viewed while the window is out of focus, so the dot stays for the person to find", async () => {
    const k = kernel([]);
    render(<App hub={k.hub} />);
    await waitFor(() => expect(k.port(MAIN)).toBeTruthy());
    focused(false);
    k.unread.add(MAIN);
    await k.emit(MAIN, { kind: "turn_started" });
    await k.emit(MAIN, { kind: "turn_done" });
    await act(async () => {});
    expect(k.port(MAIN).markSessionViewed).not.toHaveBeenCalled();
  });

  it("ignores a turn ending in a pane that is behind another", async () => {
    const k = kernel([]);
    render(<App hub={k.hub} />);
    await userEvent.click(await row(/Release notes/));
    await waitFor(() => expect(document.querySelectorAll(".pane").length).toBe(2));
    await waitFor(() => expect(k.port(RELEASE)).toBeTruthy());
    await k.emit(MAIN, { kind: "turn_done" });
    await act(async () => {});
    expect(k.port(MAIN).markSessionViewed).not.toHaveBeenCalled();
  });
});

describe("a folded project sums what is unread under it", () => {
  const fold = async (name: RegExp) => userEvent.click(await screen.findByRole("treeitem", { name }));

  it("shows a count badge with a spoken name only while the project is folded", async () => {
    const k = kernel([RELEASE, DOCS]);
    render(<App hub={k.hub} />);
    const project = await screen.findByRole("treeitem", { name: /acme/ });
    await waitFor(() => expect(within(project).queryByText("2")).toBeNull());

    await fold(/acme/);
    const shut = await screen.findByRole("treeitem", { name: /acme/ });
    expect(shut.getAttribute("aria-expanded")).toBe("false");
    expect(within(shut).getByLabelText("2 个会话未读").closest(".wsacts")).toBeTruthy();

    await fold(/acme/);
    const open = await screen.findByRole("treeitem", { name: /acme/ });
    expect(within(open).queryByLabelText(/个会话未读/)).toBeNull();
  });

  it("counts down as sessions are opened and omits archived ones", async () => {
    const k = kernel([RELEASE, DOCS]);
    render(<App hub={k.hub} />);
    await userEvent.click(await row(/Release notes/));
    await waitFor(() => expect(k.unread.has(RELEASE)).toBe(false));
    await fold(/acme/);
    const shut = await screen.findByRole("treeitem", { name: /acme/ });
    await waitFor(() => expect(within(shut).getByLabelText("1 个会话未读")).toBeTruthy());
  });

  it("shows no badge when the kernel sends no field", async () => {
    const k = kernel([RELEASE, DOCS], false);
    render(<App hub={k.hub} />);
    await fold(/acme/);
    const shut = await screen.findByRole("treeitem", { name: /acme/ });
    expect(within(shut).queryByLabelText(/个会话未读/)).toBeNull();
  });
});

describe("pane tabs carry the mark", () => {
  it("shows a dot on a tab whose session finished unseen and none on the one in front", async () => {
    const k = kernel([]);
    render(<App hub={k.hub} />);
    await userEvent.click(await row(/Release notes/));
    await waitFor(() => expect(document.querySelectorAll(".ptab").length).toBe(2));
    k.unread.add(MAIN);
    await userEvent.click(await row(/Docs sweep/));
    await waitFor(() => expect(document.querySelectorAll(".ptab").length).toBe(3));
    const tabs = screen.getAllByRole("tab");
    await waitFor(() => expect(tabs.some((tab) => within(tab).queryByRole("img", { name: "未读" }))).toBe(true));
    const active = tabs.find((tab) => tab.getAttribute("aria-selected") === "true")!;
    expect(within(active).queryByRole("img", { name: "未读" })).toBeNull();
  });

  it("clears a tab's dot when that tab is activated", async () => {
    const k = kernel([RELEASE]);
    render(<App hub={k.hub} />);
    await userEvent.click(await row(/Release notes/));
    await userEvent.click(await row(/Docs sweep/));
    await waitFor(() => expect(document.querySelectorAll(".ptab").length).toBe(3));
    k.unread.add(RELEASE);
    await act(async () => {
      await userEvent.click(screen.getAllByRole("tab")[0]);
      await userEvent.click(screen.getAllByRole("tab")[1]);
    });
    await waitFor(() => expect(k.port(RELEASE).markSessionViewed).toHaveBeenCalled());
    const release = screen.getAllByRole("tab")[1];
    expect(within(release).queryByRole("img", { name: "未读" })).toBeNull();
  });
});

describe("the mark follows the kernel across reloads", () => {
  it("shows the dot again when a later turn finishes after the person looked away", async () => {
    const k = kernel([RELEASE]);
    render(<App hub={k.hub} />);
    await userEvent.click(await row(/Release notes/));
    await waitFor(() => expect(k.unread.has(RELEASE)).toBe(false));
    await userEvent.click(await row(/Billing refactor/));
    focused(false);
    k.unread.add(RELEASE);
    await k.emit(RELEASE, { kind: "turn_started" });
    await act(async () => {});
    await k.emit(RELEASE, { kind: "turn_done" });
    await waitFor(() => expect(dotIn(screen.getByRole("treeitem", { name: /Release notes/ }))).toBeTruthy());
  });
});
