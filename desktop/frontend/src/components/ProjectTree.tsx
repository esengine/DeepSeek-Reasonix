// ProjectTree is the sidebar replacement for the flat recent-sessions list.
// It shows a tree of projects (each with expandable topics) plus a Global
// section. Clicking a topic opens its tab; "+" next to a project creates a
// new topic.
import { useCallback, useEffect, useMemo, useState } from "react";
import { ChevronRight, ChevronDown, Plus, Folder, MessageSquare, Search, BriefcaseBusiness, Trash2, Settings } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import type { ProjectNode } from "../lib/types";
import { Tooltip } from "./Tooltip";

interface ProjectTreeProps {
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => void;
}

export function ProjectTree({ activeScope, activeWorkspaceRoot, activeTopicId, onOpenTopic }: ProjectTreeProps) {
  const [tree, setTree] = useState<ProjectNode[]>([]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [newTitle, setNewTitle] = useState("");
  const [creatingUnder, setCreatingUnder] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const refresh = useCallback(async () => {
    try {
      const nodes = await app.ListProjectTree();
      const list = asArray(nodes);
      setTree(list);
      setExpanded((prev) => {
        const next = new Set(prev);
        for (const node of list) {
          if (node?.key) next.add(node.key);
        }
        return next;
      });
    } catch {
      /* bridge unavailable */
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const toggleExpand = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleCreateTopic = async (scope: string, workspaceRoot: string) => {
    if (!newTitle.trim()) {
      setCreatingUnder(null);
      return;
    }
    try {
      await app.CreateTopic(scope, workspaceRoot, newTitle.trim());
      setNewTitle("");
      setCreatingUnder(null);
      await refresh();
    } catch {
      /* ignore */
    }
  };

  const visibleTree = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return tree;
    const matches = (node: ProjectNode) =>
      [node.label, node.root, node.topicId].some((value) => (value ?? "").toLowerCase().includes(q));
    return tree
      .map((node): ProjectNode | null => {
        const children = asArray(node.children).filter(matches);
        if (matches(node) || children.length > 0) return { ...node, children };
        return null;
      })
      .filter((node): node is ProjectNode => node !== null);
  }, [query, tree]);

  const renderNode = (node: ProjectNode | null | undefined, depth: number) => {
    if (!node) return null;
    const key = node.key || `${node.kind}-${node.root ?? ""}-${node.topicId ?? ""}-${depth}`;
    const children = asArray(node.children);
    const isExpanded = expanded.has(key);
    const hasChildren = children.length > 0;

    if (node.kind === "topic" || node.kind === "global_topic") {
      const scope = node.kind === "global_topic" ? "global" : "project";
      const active =
        activeTopicId === node.topicId &&
        activeScope === scope &&
        (scope === "global" || activeWorkspaceRoot === node.root);
      const label = (node.label || node.topicId || "Untitled").replace(/^●\s*/, "");
      return (
        <button
          key={key}
          className={`project-tree__topic${active ? " project-tree__topic--active" : ""}`}
          style={{ paddingLeft: 16 + depth * 16 }}
          onClick={() => onOpenTopic(scope, node.root ?? "", node.topicId ?? "")}
        >
          <MessageSquare size={12} />
          <span className="project-tree__topic-label">{label}</span>
          {active && <span className="project-tree__active-mark" />}
        </button>
      );
    }

    return (
      <div key={key}>
        <div
          className="project-tree__folder"
          style={{ paddingLeft: 8 + depth * 16 }}
        >
          <button
            className="project-tree__folder-toggle"
            onClick={() => toggleExpand(key)}
          >
            {hasChildren ? (
              isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />
            ) : (
              <span style={{ width: 12 }} />
            )}
          </button>
          <Folder size={12} />
          <span className="project-tree__folder-label">{node.label || "Untitled"}</span>
          <Tooltip label="New topic">
            <button
              className="project-tree__new-topic"
              onClick={(e) => {
                e.stopPropagation();
                setCreatingUnder(key);
                setNewTitle("");
              }}
            >
              <Plus size={12} />
            </button>
          </Tooltip>
        </div>
        {creatingUnder === key && (
          <div className="project-tree__new-input" style={{ paddingLeft: 28 + depth * 16 }}>
            <input
              autoFocus
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  void handleCreateTopic(
                    node.kind === "global_folder" ? "global" : "project",
                    node.root ?? "",
                  );
                }
                if (e.key === "Escape") setCreatingUnder(null);
              }}
              onBlur={() => {
                if (!newTitle.trim()) setCreatingUnder(null);
                else void handleCreateTopic(
                  node.kind === "global_folder" ? "global" : "project",
                  node.root ?? "",
                );
              }}
              placeholder="Topic name"
            />
          </div>
        )}
        {isExpanded && hasChildren && (
          <div className="project-tree__children">
            {children.map((child) => renderNode(child, depth + 1))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="project-tree">
      <label className="project-tree__search">
        <Search size={14} />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="搜索项目或主题"
        />
      </label>
      <div className="project-tree__header">
        <span className="project-tree__header-title">
          <BriefcaseBusiness size={13} />
          项目工作区
        </span>
      </div>
      <div className="project-tree__list">
        {visibleTree.length === 0 ? (
          <div className="project-tree__empty">{query.trim() ? "没有匹配的项目或主题" : "还没有项目，点击顶部 + 打开项目"}</div>
        ) : (
          visibleTree.map((node) => renderNode(node, 0))
        )}
      </div>
      <div className="project-tree__other">
        <div className="project-tree__other-title">其他</div>
        <button type="button" className="project-tree__other-item">
          <Trash2 size={13} />
          回收站
        </button>
        <button type="button" className="project-tree__other-item">
          <Settings size={13} />
          设置
        </button>
      </div>
    </div>
  );
}
