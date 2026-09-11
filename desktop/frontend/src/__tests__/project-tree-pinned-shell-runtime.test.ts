import { mergeProjectTopicPage, projectTreeShellChildren, splitPinnedProjectTree } from "../components/ProjectTree";

type Equal = (actual: unknown, expected: unknown, label: string) => void;

export function runProjectTreePinnedShellRuntimeTests(eq: Equal, projectTreeSource: string) {
  eq(
    splitPinnedProjectTree(
      [{
        key: "global_folder",
        kind: "global_folder",
        label: "Global",
        children: [
          { key: "global_topic_duplicate", kind: "global_topic", label: "Pinned", topicId: "duplicate", pinned: true },
          { key: "global_topic_duplicate_page", kind: "global_topic", label: "Pinned", topicId: "duplicate" },
          // A live runtime projection can carry the same topic as a session row
          // while the catalog still serves the original topic shell.
          { key: "global_session_duplicate", kind: "global_session", label: "Pinned", topicId: "duplicate" },
        ],
      }],
      "updated",
      false,
    ),
    {
      pinned: [
        { key: "global_topic_duplicate", kind: "global_topic", label: "Pinned", topicId: "duplicate", pinned: true },
      ],
      projects: [{ key: "global_folder", kind: "global_folder", label: "Global", children: [] }],
    },
    "pinned conversations do not repeat as runtime session rows in their source folder",
  );

  eq(
    splitPinnedProjectTree(
      [
        {
          key: "project_a",
          kind: "project",
          label: "A",
          root: "/repo/a",
          children: [{ key: "topic_a", kind: "topic", label: "Pinned A", root: "/repo/a", topicId: "shared", pinned: true }],
        },
        {
          key: "project_b",
          kind: "project",
          label: "B",
          root: "/repo/b",
          children: [{ key: "topic_b", kind: "topic", label: "Regular B", root: "/repo/b", topicId: "shared" }],
        },
      ],
      "updated",
      false,
    ).projects.map((project) => asTopicKeys(project.children)),
    [[], ["topic_b"]],
    "a pinned topic does not hide the same topic ID from another project",
  );

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

function asTopicKeys(children: { key: string }[] | undefined): string[] {
  return children?.map((node) => node.key) ?? [];
}
