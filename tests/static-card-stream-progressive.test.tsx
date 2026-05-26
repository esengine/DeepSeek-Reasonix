/** Progressive `<Static>` priming — first paint shows a small chunk; backlog
 *  drains via setImmediate so the event loop stays responsive. */

import { type ComponentType, type ReactElement, createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { StaticCardStream } from "../src/cli/ui/layout/StaticCardStream.js";
import type { UserCard } from "../src/cli/ui/state/cards.js";
import { AgentStoreProvider } from "../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../src/cli/ui/state/state.js";
import { render } from "./helpers/ink-test.js";

let lastStaticItems: unknown[] = [];

vi.mock("ink", async () => {
  const actual = await vi.importActual<typeof import("ink")>("ink");
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    ...actual,
    Static: (props: Record<string, unknown>) => {
      lastStaticItems = (props.items as unknown[]) ?? [];
      return React.createElement(actual.Static as ComponentType<Record<string, unknown>>, props);
    },
  };
});

const SESSION: SessionInfo = {
  id: "s-progressive",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function userCard(i: number): UserCard {
  return { id: `u-${i}`, ts: i, kind: "user", text: `prompt ${i}` };
}

async function flushImmediates(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
}

describe("StaticCardStream progressive mount", () => {
  it("releases all cards immediately when backlog ≤ initial batch", () => {
    lastStaticItems = [];
    const cards = Array.from({ length: 10 }, (_, i) => userCard(i));
    const { unmount } = render(
      createElement(
        AgentStoreProvider,
        { session: SESSION, initialCards: cards },
        createElement(StaticCardStream),
      ),
    );
    expect(lastStaticItems.length).toBe(10);
    unmount();
  });

  it("releases the initial batch on first render and drains the rest in batches", async () => {
    lastStaticItems = [];
    const cards = Array.from({ length: 200 }, (_, i) => userCard(i));
    const { rerender, unmount } = render(
      createElement(
        AgentStoreProvider,
        { session: SESSION, initialCards: cards },
        createElement(StaticCardStream),
      ),
    );
    expect(lastStaticItems.length).toBeLessThan(200);
    expect(lastStaticItems.length).toBeGreaterThan(0);

    for (let i = 0; i < 12 && lastStaticItems.length < 200; i++) {
      await flushImmediates();
      rerender(
        createElement(
          AgentStoreProvider,
          { session: SESSION, initialCards: cards },
          createElement(StaticCardStream),
        ),
      );
    }
    expect(lastStaticItems.length).toBe(200);
    unmount();
  });

  it("preserves chronological order across the drain (no card index reorder)", async () => {
    lastStaticItems = [];
    const cards = Array.from({ length: 100 }, (_, i) => userCard(i));
    const { rerender, unmount } = render(
      createElement(
        AgentStoreProvider,
        { session: SESSION, initialCards: cards },
        createElement(StaticCardStream),
      ),
    );
    for (let i = 0; i < 8 && lastStaticItems.length < 100; i++) {
      await flushImmediates();
      rerender(
        createElement(
          AgentStoreProvider,
          { session: SESSION, initialCards: cards },
          createElement(StaticCardStream),
        ),
      );
    }
    const ids = lastStaticItems.map((c) => (c as UserCard).id);
    expect(ids).toEqual(cards.map((c) => c.id));
    unmount();
  });
});
