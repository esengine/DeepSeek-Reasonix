import { mergeProjectTopicPage, projectTreeShellChildren } from "../components/ProjectTree";

type Equal = (actual: unknown, expected: unknown, label: string) => void;

export function runProjectTreePinnedShellRuntimeTests(eq: Equal, projectTreeSource: string) {
  eq(
    mergeProjectTopicPage(
      [
        { key: "topic-off-page-pin", kind: "topic", label: "Pinned", topicId: "pin", pinned: true },
        { key: "topic-stale", kind: "topic", label: "Stale", topicId: "stale" },
      ],
      [{ key: "topic-page", kind: "topic", label: "First page", topicId: "page" }],
      false,
    ).map((node) => node.key),
    ["topic-page", "topic-off-page-pin"],
    "a bounded first page keeps pinned shells that fall beyond the page limit",
  );

  eq(
    projectTreeShellChildren(undefined, [
      { key: "topic_pin", kind: "topic", label: "Pinned", topicId: "pin", pinned: true },
    ]).map((node) => node.topicId),
    ["pin"],
    "a cold collapsed tree paints pinned topic shells before any folder page loads",
  );

  eq(
    projectTreeShellChildren(
      [
        { key: "topic_old", kind: "topic", label: "Old pin", topicId: "old", pinned: true },
        { key: "topic_regular", kind: "topic", label: "Regular", topicId: "regular" },
      ],
      [{ key: "topic_new", kind: "topic", label: "New pin", topicId: "new", pinned: true }],
    ).map((node) => ({ topicId: node.topicId, pinned: Boolean(node.pinned) })),
    [
      { topicId: "old", pinned: false },
      { topicId: "regular", pinned: false },
      { topicId: "new", pinned: true },
    ],
    "shell refresh removes stale pins and adds newly pinned collapsed topics",
  );

  eq(
    projectTreeSource.includes("children: projectTreeShellChildren(previous?.children, project.children)"),
    true,
    "shell refresh preserves loaded folders while reconciling pinned topic shells",
  );
}
