// @vitest-environment jsdom
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { MockPort } from "../port/mock";
import { WorktreeChip } from "./WorktreeChip";
import type { AgentPort } from "../port/port";

const root = document.createElement("div");
let mounted: ReturnType<typeof createRoot> | null = null;
afterEach(() => { mounted?.unmount(); mounted = null; root.remove(); delete (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT; });

it("keeps open a menu opened between the chip's commit and its mount effects", async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = false;
  document.body.append(root);
  const port = new MockPort() as AgentPort;
  const sources = { available: false, sourceRoot: "/w", sources: [] };
  mounted = createRoot(root);
  const clicked = new Promise<void>((resolve) => {
    const watch = new MutationObserver(() => {
      const button = root.querySelector<HTMLButtonElement>("button.worktree-trigger");
      if (!button) return;
      watch.disconnect();
      button.click();
      resolve();
    });
    watch.observe(root, { childList: true, subtree: true });
  });
  mounted.render(<WorktreeChip port={port} sources={sources} canCreate={false} selection={null} branch="main" busy={false}
    changeCount={0} onSelect={vi.fn()} refresh={async () => sources} onChanged={vi.fn()} onError={vi.fn()} />);
  await clicked;
  await new Promise((resolve) => setTimeout(resolve, 100));
  expect(root.querySelector("button.worktree-trigger")?.getAttribute("aria-expanded")).toBe("true");
});
