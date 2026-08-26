import type { RemoteTabRefView } from "../lib/types";
import type { ShortcutPlatform } from "../lib/keyboardShortcuts";
import type { TopicShortcutEntry } from "../lib/topicShortcuts";
import type { ProjectTreeVariant } from "../lib/projectTreeTopic";

type ProjectTreeImTopicSource = {
  platform?: string;
  label: string;
  title?: string;
  remoteId?: string;
};

export interface ProjectTreeProps {
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  activeSessionPath?: string;
  activeRemote?: RemoteTabRefView;
  imTopicSources?: Record<string, ProjectTreeImTopicSource>;
  variant?: ProjectTreeVariant;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string, sessionPath?: string) => Promise<void> | void;
  onAddProject: (path?: string) => Promise<void>;
  onCreateTopic?: (scope: string, workspaceRoot: string) => Promise<void> | void;
  onCreateIsolatedWorktree?: (workspaceRoot: string) => Promise<void> | void;
  onRenameTopic?: (topicId: string, title: string) => Promise<void> | void;
  onTopicsChanged?: () => Promise<void> | void;
  refreshSignal?: number;
  timeFilter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";
  onTimeFilterChange: (filter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d") => void;
  searchExpanded?: boolean;
  searchFocusSignal?: number;
  showShortcutBadges?: boolean;
  shortcutPlatform?: ShortcutPlatform;
  onVisibleTopicsChange?: (topics: TopicShortcutEntry[]) => void;
}
