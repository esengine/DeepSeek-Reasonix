// Run: tsx src/__tests__/project-tree-runtime.test.ts

import {
  projectTreeFolderDisclosure,
  defaultExpandedProjectTreeKeys,
  activeSessionAncestorKeys,
  projectTreeTopicOpenRequest,
  projectTreeShouldSuppressOpenForRename,
  projectTreeReadActivityKey,
  projectTreeTopicHasUnreadActivity,
  projectTreeTopicArchiveBlocked,
  projectTreeShouldRenderTopicActions,
  projectTreeTopicMetaLine,
  arrangeClassicProjectTree,
  splitPinnedProjectTree,
  classicTopicWindow,
  projectTreeTopicHoverCardModel,
  projectTreeTopicMenuOffersPin,
  projectTreeDedupedExactTime,
} from "../components/ProjectTree";
import type { ProjectNode } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nproject tree runtime sessions");

const testT = (key: string, vars?: Record<string, string | number>) => {
  if (key === "history.turnOne") return `${vars?.n ?? 1} turn`;
  if (key === "history.turnOther") return `${vars?.n ?? 0} turns`;
  return key;
};

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
          {
            key: "global_session_b",
            kind: "global_session",
            label: "Session B",
            topicId: "topic-a",
            sessionPath: "/tmp/b.jsonl",
          },
        ],
      },
      {
        key: "global_topic_topic-b",
        kind: "global_topic",
        label: "Topic B",
        topicId: "topic-b",
      },
    ],
  },
];

eq(
  defaultExpandedProjectTreeKeys(tree),
  [],
  "without an active tab, no folders default to expanded",
);

eq(
  defaultExpandedProjectTreeKeys(tree, "global", "", "topic-a", "/tmp/b.jsonl"),
  ["global_folder", "global_topic_topic-a"],
  "active session path expands only ancestor folders",
);

eq(
  activeSessionAncestorKeys(tree, "global", "", "topic-a", "/tmp/b.jsonl"),
  ["global_folder", "global_topic_topic-a"],
  "activeSessionAncestorKeys matches defaultExpandedProjectTreeKeys for active session",
);

eq(
  activeSessionAncestorKeys(tree, "global", "", "topic-b"),
  ["global_folder"],
  "active topic without runtime session rows expands only parent folders",
);

// Waiting confirmation copy is stable for both compact and expanded sidebars.
eq(
  testT("projectTree.status.waitingConfirmation"),
  "projectTree.status.waitingConfirmation",
  "waiting confirmation key stays available for side-bar pill and badge",
);

eq(
  projectTreeTopicOpenRequest(tree[0].children?.[0].children?.[1] as ProjectNode),
  { scope: "global", workspaceRoot: "", topicId: "topic-a", sessionPath: "/tmp/b.jsonl" },
  "runtime session row opens the concrete session path",
);

eq(
  projectTreeTopicOpenRequest({
    key: "topic_project",
    kind: "topic",
    label: "Project topic",
    root: "/repo",
    topicId: "topic-project",
  }),
  { scope: "project", workspaceRoot: "/repo", topicId: "topic-project", sessionPath: undefined },
  "regular project topic still opens by topic",
);

eq(
  projectTreeTopicMetaLine({
    key: "global_topic_missing_time",
    kind: "global_topic",
    label: "Old empty topic",
    topicId: "missing-time",
  }, testT),
  "projectTree.previously",
  "topic with no turns and no timestamps renders previous-time fallback meta",
);

eq(
  projectTreeTopicMetaLine({
    key: "global_topic_recent",
    kind: "global_topic",
    label: "Recent blank topic",
    topicId: "recent",
    createdAt: Date.now(),
  }, testT),
  "projectTree.justNow",
  "topic with a real recent timestamp still renders just-now meta",
);

const completedTopic: ProjectNode = {
  key: "topic_complete",
  kind: "topic",
  label: "Completed",
  root: "/repo",
  topicId: "topic-complete",
  lastActivityAt: 2000,
};
const completedTopicKey = projectTreeReadActivityKey(completedTopic) ?? "";

eq(
  projectTreeTopicHasUnreadActivity(completedTopic, { [completedTopicKey]: 1000 }, "project", "/repo", "other-topic"),
  true,
  "completed inactive topic with newer activity shows unread attention",
);

eq(
  projectTreeTopicHasUnreadActivity(completedTopic, { [completedTopicKey]: 2000 }, "project", "/repo", "other-topic"),
  false,
  "completed topic stops showing unread attention once opened at its latest activity",
);

eq(
  projectTreeTopicHasUnreadActivity(completedTopic, { [completedTopicKey]: 1000 }, "project", "/repo", "topic-complete"),
  false,
  "active topic does not show unread attention",
);

eq(
  projectTreeTopicHasUnreadActivity({ ...completedTopic, status: "streaming", running: true }, { [completedTopicKey]: 1000 }, "project", "/repo", "other-topic"),
  false,
  "running topic keeps runtime status instead of completed-unread attention",
);

for (const status of ["thinking", "streaming", "waiting_confirmation", "background_job"] as const) {
  eq(
    projectTreeTopicArchiveBlocked({ ...completedTopic, status, running: true }),
    true,
    `${status} topic blocks archive`,
  );
}

for (const status of ["paused", "error"] as const) {
  eq(
    projectTreeTopicArchiveBlocked({ ...completedTopic, status, running: true }),
    false,
    `${status} topic remains archivable despite the legacy running flag`,
  );
}

eq(
  projectTreeTopicArchiveBlocked({ ...completedTopic, running: true }),
  true,
  "legacy running topic without a detailed status blocks archive",
);

eq(
  projectTreeTopicArchiveBlocked(completedTopic),
  false,
  "idle topic remains archivable",
);

eq(
  projectTreeTopicArchiveBlocked({
    ...completedTopic,
    children: [
      { ...completedTopic, key: "idle-child", kind: "session", sessionPath: "/tmp/idle.jsonl" },
      { ...completedTopic, key: "busy-child", kind: "session", sessionPath: "/tmp/busy.jsonl", status: "background_job", running: true },
    ],
  }),
  true,
  "topic blocks archive when any runtime child has active work",
);

eq(
  projectTreeShouldRenderTopicActions(false, "workbench", false),
  true,
  "read workbench topic renders hover actions",
);

eq(
  projectTreeShouldRenderTopicActions(false, "classic", false),
  true,
  "read classic topic renders hover actions",
);

eq(
  projectTreeShouldRenderTopicActions(false, "classic", true),
  false,
  "unread classic topic reserves the action column for unread attention",
);

eq(
  projectTreeShouldRenderTopicActions(false, "creation", false),
  false,
  "creation topic keeps hover actions disabled",
);

eq(
  projectTreeShouldRenderTopicActions(true, "classic", false),
  false,
  "runtime session rows do not render topic hover actions",
);

eq(
  projectTreeShouldSuppressOpenForRename(
    { rowKey: "topic-a", canRename: true },
    { rowKey: "topic-a", canRename: true },
  ),
  true,
  "second click on the same renameable topic suppresses open for inline rename",
);

eq(
  projectTreeShouldSuppressOpenForRename(
    { rowKey: "session-a", canRename: false },
    { rowKey: "session-a", canRename: false },
  ),
  false,
  "runtime session double-click still allows the session row to open",
);

eq(
  projectTreeShouldSuppressOpenForRename(
    { rowKey: "topic-a", canRename: true },
    { rowKey: "topic-b", canRename: true },
  ),
  false,
  "quickly clicking a different topic still opens the new target",
);

eq(
  projectTreeFolderDisclosure(false, true),
  {
    canExpand: false,
    isOpen: false,
    ariaExpanded: undefined,
    iconStackClassName: "project-tree__icon-stack",
  },
  "empty project folders are not exposed as expandable disclosure rows",
);

eq(
  projectTreeFolderDisclosure(true, false),
  {
    canExpand: true,
    isOpen: false,
    ariaExpanded: false,
    iconStackClassName: "project-tree__icon-stack project-tree__icon-stack--expandable",
  },
  "collapsed project folders keep disclosure semantics when children exist",
);

eq(
  projectTreeFolderDisclosure(true, true),
  {
    canExpand: true,
    isOpen: true,
    ariaExpanded: true,
    iconStackClassName: "project-tree__icon-stack project-tree__icon-stack--expandable",
  },
  "expanded project folders can show the open-folder state only when children exist",
);

console.log("\nclassic project tree sorting");

const classicTopic = (id: string, extra: Partial<ProjectNode> = {}): ProjectNode => ({
  key: `topic_${id}`,
  kind: "topic",
  label: id,
  root: "/repo/a",
  topicId: id,
  ...extra,
});

const classicTree: ProjectNode[] = [
  {
    key: "project_/repo/a",
    kind: "project",
    label: "a",
    root: "/repo/a",
    children: [
      classicTopic("old", { lastActivityAt: 100 }),
      classicTopic("newest", { lastActivityAt: 300 }),
      classicTopic("blank", { createdAt: 200 }),
    ],
  },
  {
    key: "project_/repo/b",
    kind: "project",
    label: "b",
    root: "/repo/b",
    children: [classicTopic("only", { root: "/repo/b", lastActivityAt: 50 })],
  },
];

eq(
  arrangeClassicProjectTree(classicTree, "updated").map((node) => (node.children ?? []).map((child) => child.topicId)),
  [["newest", "blank", "old"], ["only"]],
  "classic default sorts topics by last activity while keeping project order",
);

eq(
  arrangeClassicProjectTree(
    [
      {
        key: "project_/repo/a",
        kind: "project",
        label: "a",
        root: "/repo/a",
        children: [
          classicTopic("created-first", { createdAt: 100, lastActivityAt: 900 }),
          classicTopic("created-last", { createdAt: 500, lastActivityAt: 600 }),
        ],
      },
    ],
    "created",
  ).map((node) => (node.children ?? []).map((child) => child.topicId)),
  [["created-last", "created-first"]],
  "classic created mode sorts topics by creation time",
);

eq(
  arrangeClassicProjectTree(
    [
      {
        key: "project_/repo/a",
        kind: "project",
        label: "a",
        root: "/repo/a",
        children: [
          classicTopic("recent", { lastActivityAt: 900 }),
          classicTopic("pinned-old", { lastActivityAt: 100, pinned: true }),
        ],
      },
    ],
    "updated",
  ).map((node) => (node.children ?? []).map((child) => child.topicId)),
  [["pinned-old", "recent"]],
  "classic sorting keeps pinned topics above unpinned ones",
);

const classicPinnedSections = splitPinnedProjectTree(
  [
    {
      key: "project_/repo/a",
      kind: "project",
      label: "a",
      root: "/repo/a",
      children: [
        classicTopic("pinned-old", { lastActivityAt: 100, pinned: true }),
        classicTopic("recent", { lastActivityAt: 900 }),
      ],
    },
    {
      key: "project_/repo/b",
      kind: "project",
      label: "b",
      root: "/repo/b",
      pinned: true,
      children: [classicTopic("pinned-new", { root: "/repo/b", lastActivityAt: 500, pinned: true })],
    },
  ],
  "updated",
  false,
);

eq(
  classicPinnedSections.pinned.map((node) => node.topicId),
  ["pinned-new", "pinned-old"],
  "classic pinned section collects topics across projects by activity",
);

eq(
  classicPinnedSections.projects.map((node) => ({
    root: node.root,
    pinned: Boolean(node.pinned),
    topics: (node.children ?? []).map((child) => child.topicId),
  })),
  [
    { root: "/repo/a", pinned: false, topics: ["recent"] },
    { root: "/repo/b", pinned: true, topics: [] },
  ],
  "classic pinned topics appear once while pinned projects stay in project order",
);

eq(
  splitPinnedProjectTree(
    [
      {
        key: "project_/repo/a",
        kind: "project",
        label: "a",
        root: "/repo/a",
        children: [classicTopic("unpinned-again", { lastActivityAt: 100 })],
      },
    ],
    "updated",
    false,
  ),
  {
    pinned: [],
    projects: [
      {
        key: "project_/repo/a",
        kind: "project",
        label: "a",
        root: "/repo/a",
        children: [classicTopic("unpinned-again", { lastActivityAt: 100 })],
      },
    ],
  },
  "unpinning returns a topic to its original project",
);

eq(
  splitPinnedProjectTree(
    [{ key: "project_/repo/a", kind: "project", label: "a", root: "/repo/a", pinned: true, children: [] }],
    "updated",
  ).pinned.map((node) => node.root),
  ["/repo/a"],
  "workbench pinned section still extracts pinned projects",
);

console.log("\nclassic topic window and hover card");

const windowTopics = Array.from({ length: 7 }, (_, i) => classicTopic(`t${i}`, { lastActivityAt: 1000 - i }));

eq(
  (() => {
    const { visible, hiddenCount } = classicTopicWindow(windowTopics, false);
    return { ids: visible.map((node) => node.topicId), hiddenCount };
  })(),
  { ids: ["t0", "t1", "t2", "t3", "t4"], hiddenCount: 2 },
  "classic window previews the first five topics and reports the hidden count",
);

eq(
  (() => {
    const { visible, hiddenCount } = classicTopicWindow(windowTopics, true);
    return { count: visible.length, hiddenCount };
  })(),
  { count: 7, hiddenCount: 0 },
  "classic window shows everything once the folder is toggled open",
);

eq(
  classicTopicWindow(windowTopics.slice(0, 4), false),
  { visible: windowTopics.slice(0, 4), hiddenCount: 0 },
  "classic window leaves short folders untouched",
);

eq(
  projectTreeTopicHoverCardModel(
    { key: "topic_t", kind: "topic", label: "● Busy topic", root: "/repo", topicId: "t", turns: 3, status: "streaming" },
    testT,
    "my-project",
  ),
  {
    title: "Busy topic",
    statusLabel: "projectTree.status.streaming",
    metaLine: "3 turns",
    exactTime: "",
    projectLabel: "my-project",
  },
  "hover card model strips the running marker and carries turns, status, and project",
);

const day = 24 * 60 * 60 * 1000;

eq(
  (() => {
    const card = projectTreeTopicHoverCardModel(
      { key: "topic_old", kind: "topic", label: "Old topic", root: "/repo", topicId: "old", turns: 3, lastActivityAt: Date.now() - 30 * day },
      testT,
      "my-project",
    );
    return { exactTime: card.exactTime, metaHasTurns: card.metaLine.startsWith("3 turns · ") };
  })(),
  { exactTime: "", metaHasTurns: true },
  "hover card keeps a single calendar-date copy for week-old sessions",
);

eq(
  (() => {
    const card = projectTreeTopicHoverCardModel(
      { key: "topic_recent", kind: "topic", label: "Recent topic", root: "/repo", topicId: "recent", turns: 2, lastActivityAt: Date.now() - 2 * day },
      testT,
      "my-project",
    );
    return { hasExactTime: card.exactTime.length > 0, metaRepeatsDate: card.metaLine.includes(card.exactTime) };
  })(),
  { hasExactTime: true, metaRepeatsDate: false },
  "hover card for recent sessions still pairs relative time with the exact date",
);

eq(
  projectTreeDedupedExactTime("3 turns · 2026/7/7", "2026/7/7"),
  "",
  "row title and hover card drop the exact date the meta line already ends with",
);

eq(
  projectTreeDedupedExactTime("3 turns · 2 days ago", "2026/7/12"),
  "2026/7/12",
  "recent sessions keep the exact date next to the relative meta line",
);

eq(
  projectTreeTopicMenuOffersPin("classic"),
  true,
  "classic context menu offers the pin entry",
);

eq(
  projectTreeTopicMenuOffersPin("workbench"),
  true,
  "workbench context menu keeps the pin entry",
);

eq(
  projectTreeTopicMenuOffersPin("creation"),
  false,
  "creation context menu hides pin so the shared ordering stays untouched",
);

eq(
  projectTreeFolderDisclosure(false, false, true),
  {
    canExpand: true,
    isOpen: false,
    ariaExpanded: false,
    iconStackClassName: "project-tree__icon-stack project-tree__icon-stack--expandable",
  },
  "classic empty folders stay expandable so the placeholder row is reachable",
);

eq(
  projectTreeFolderDisclosure(false, true, true),
  {
    canExpand: true,
    isOpen: true,
    ariaExpanded: true,
    iconStackClassName: "project-tree__icon-stack project-tree__icon-stack--expandable",
  },
  "expanded classic empty folders report the open state for the placeholder",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
