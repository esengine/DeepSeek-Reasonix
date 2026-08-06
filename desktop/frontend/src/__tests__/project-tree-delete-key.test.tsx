// Run: node --import tsx src/__tests__/project-tree-delete-key.test.tsx
//
// Delete-key handling for sidebar topics (issue #3220): pressing Delete while
// a sidebar topic button is focused arms the same in-menu confirmation as the
// right-click delete entry ("Confirm move to trash"), and the second press (or
// clicking that confirm entry) trashes the topic through the existing path.
// With no sidebar item focused — or with a session node / running topic, which
// the right-click menu also excludes — Delete must do nothing.

import { JSDOM } from "jsdom";
import React from "react";
import type { ProjectNode } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nproject tree delete key");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });

// Bridge mock: the app proxy in lib/bridge reads window.go.main.App lazily on
// every call, so wiring it before importing the component is enough.
const trashCalls: string[] = [];
const tree: ProjectNode[] = [
  {
    key: "global_folder",
    kind: "global_folder",
    label: "Global",
    children: [
      {
        key: "global_topic_topic-a",
        kind: "global_topic",
        label: "Topic A",
        topicId: "topic-a",
        children: [
          {
            key: "global_session_a",
            kind: "global_session",
            label: "Session A",
            topicId: "topic-a",
            sessionPath: "/tmp/a.jsonl",
          },
        ],
      },
      {
        key: "global_topic_topic-b",
        kind: "global_topic",
        label: "Topic B",
        topicId: "topic-b",
      },
      {
        key: "global_topic_topic-c",
        kind: "global_topic",
        label: "Topic C",
        topicId: "topic-c",
        running: true,
      },
    ],
  },
];
window.go = {
  main: {
    App: {
      async ListProjectTree() {
        return tree;
      },
      async Platform() {
        return "linux";
      },
      async TrashTopic(topicId: string) {
        trashCalls.push(topicId);
      },
      async DeliveryWorktreeAvailability() {
        return { available: false };
      },
      async SetTopicPinned() {},
      async RenameTopic() {},
      async ReorderProjects() {},
      async RemoveWorkspace() {},
      async RenameProject() {},
      async SetProjectColor() {},
      async SetProjectPinned() {},
      async RevealPath() {},
      async CreateTopic() {
        return { id: "created" };
      },
    },
  },
};

const [{ createRoot }, { act }, { ProjectTree }, { LocaleProvider }, { ToastProvider }] = await Promise.all([
  import("react-dom/client"),
  import("react"),
  import("../components/ProjectTree"),
  import("../lib/i18n"),
  import("../lib/toast"),
]);

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 30));
}

await act(async () => {
  root.render(
    <LocaleProvider>
      <ToastProvider>
        <ProjectTree
          variant="classic"
          activeTopicId="topic-a"
          onOpenTopic={() => {}}
          onAddProject={async () => {}}
          onCreateTopic={() => {}}
          onCreateDeliveryWorktree={() => {}}
          onRenameTopic={async () => {}}
          onTopicsChanged={async () => {}}
          timeFilter="all"
          onTimeFilterChange={() => {}}
        />
      </ToastProvider>
    </LocaleProvider>,
  );
  await flush();
});

const topicButton = (label: string) =>
  Array.from(document.querySelectorAll<HTMLButtonElement>(".project-tree__topic-main"))
    .find((button) => button.textContent?.includes(label));

const confirmMenuOpen = () => {
  const menu = document.querySelector(".context-menu");
  return menu !== null && menu.textContent?.includes("Confirm move to trash") === true;
};
const anyMenuOpen = () => document.querySelector(".context-menu") !== null;

// 1. No sidebar item focused → Delete is a no-op.
await act(async () => {
  document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true }));
  await flush();
});
ok(!anyMenuOpen(), "Delete with no focused sidebar item opens no menu");
eq(trashCalls, [], "Delete with no focused sidebar item trashes nothing");

// 2. Delete on the focused topic arms the same in-menu confirmation as the
//    right-click delete entry (first press: confirm state, nothing trashed).
const topicA = topicButton("Topic A");
ok(topicA !== undefined, "topic button renders in the sidebar");
if (topicA) {
  await act(async () => {
    topicA.focus();
    topicA.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await flush();
  });
  ok(document.activeElement === topicA, "topic button holds focus while Delete is pressed");
  ok(confirmMenuOpen(), "first Delete on the focused topic opens the confirm menu");
  eq(trashCalls, [], "first Delete only arms confirmation, it does not trash yet");

  // 3. Second Delete confirms through the existing delete path.
  await act(async () => {
    topicA.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await flush();
  });
  eq(trashCalls, ["topic-a"], "second Delete on the focused topic trashes it via the existing path");
  ok(!anyMenuOpen(), "confirming closes the menu");

  // 4. Escape cancels an armed confirmation: nothing trashed, menu closed,
  //    and a later Delete re-arms instead of trashing.
  await act(async () => {
    topicA.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await flush();
  });
  ok(confirmMenuOpen(), "Delete after a trash re-arms the confirmation menu");
  await act(async () => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flush();
  });
  ok(!anyMenuOpen(), "Escape closes the armed menu");
  eq(trashCalls, ["topic-a"], "cancelling via Escape does not trash");
  await act(async () => {
    topicA.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await flush();
  });
  eq(trashCalls, ["topic-a"], "Delete after a cancel re-arms confirmation instead of trashing");
  ok(confirmMenuOpen(), "Delete after a cancel shows the confirm entry again");

  // 5. Clicking the shared menu confirm entry trashes too (same path as the
  //    right-click menu).
  const confirmItem = Array.from(document.querySelectorAll<HTMLButtonElement>(".context-menu__item"))
    .find((button) => button.textContent?.includes("Confirm move to trash"));
  ok(confirmItem !== undefined, "the armed menu exposes the shared confirm entry");
  await act(async () => {
    confirmItem?.click();
    await flush();
  });
  eq(trashCalls, ["topic-a", "topic-a"], "clicking the menu confirm entry trashes through the shared path");

  // 6. Session nodes have no delete entry in the right-click menu; Delete
  //    must leave them alone.
  await act(async () => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flush();
  });
  const sessionA = topicButton("Session A");
  ok(sessionA !== undefined, "session node renders in the sidebar");
  if (sessionA) {
    await act(async () => {
      sessionA.focus();
      sessionA.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
      await flush();
    });
    ok(!anyMenuOpen(), "Delete on a session node opens no menu");
    eq(trashCalls, ["topic-a", "topic-a"], "Delete on a session node trashes nothing");
  }

  // 7. Running topics are excluded from delete (the menu entry is disabled);
  //    Delete must leave them alone too.
  const topicC = topicButton("Topic C");
  ok(topicC !== undefined, "running topic renders in the sidebar");
  if (topicC) {
    await act(async () => {
      topicC.focus();
      topicC.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
      await flush();
    });
    ok(!anyMenuOpen(), "Delete on a running topic opens no menu");
    eq(trashCalls, ["topic-a", "topic-a"], "Delete on a running topic trashes nothing");
  }
}

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
