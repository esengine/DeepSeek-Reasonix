import { asArray } from "./array";
import type { ProjectNode, ProjectTreeOrganizationBindings, SessionGroup } from "./types";

type OrganizationBindings = ProjectTreeOrganizationBindings;

const groupsByKey: Record<string, SessionGroup[]> = {};
const groupRevisionsByKey: Record<string, number> = {};

function organizationKey(scope: string, workspaceRoot: string): string {
  return scope === "global" ? "global|" : `project|${workspaceRoot}`;
}

export function makeMockProjectTreeOrganizationBindings(tree: ProjectNode[]): OrganizationBindings {
  return {
    async ReorderTopics(scope, workspaceRoot, orderedTopicIDs) {
      const parent = tree.find((node) => scope === "global"
        ? node.kind === "global_folder"
        : node.kind === "project" && node.root === workspaceRoot);
      if (!parent) return;
      const children = asArray(parent.children), byID = new Map(children.filter((node) => node.topicId).map((node) => [node.topicId!, node]));
      const ordered = orderedTopicIDs.map((id) => byID.get(id)).filter((node): node is ProjectNode => Boolean(node));
      const seen = new Set(orderedTopicIDs);
      parent.children = [...ordered, ...children.filter((node) => !node.topicId || !seen.has(node.topicId))];
    },
    async ListProjectGroups(scope, workspaceRoot) {
      return structuredClone(groupsByKey[organizationKey(scope, workspaceRoot)] ?? []);
    },
    async SaveSessionGroups(scope, workspaceRoot, groups) {
      const key = organizationKey(scope, workspaceRoot);
      groupsByKey[key] = structuredClone(groups);
      groupRevisionsByKey[key] = (groupRevisionsByKey[key] ?? 0) + 1;
    },
    async GetProjectGroups(scope, workspaceRoot) {
      const key = organizationKey(scope, workspaceRoot);
      return { groups: structuredClone(groupsByKey[key] ?? []), revision: groupRevisionsByKey[key] ?? 0, applied: true };
    },
    async SaveSessionGroupsVersioned(scope, workspaceRoot, expectedRevision, groups) {
      const key = organizationKey(scope, workspaceRoot), revision = groupRevisionsByKey[key] ?? 0;
      if (revision !== expectedRevision) {
        return { groups: structuredClone(groupsByKey[key] ?? []), revision, applied: false };
      }
      groupsByKey[key] = structuredClone(groups);
      groupRevisionsByKey[key] = revision + 1;
      return { groups: structuredClone(groups), revision: revision + 1, applied: true };
    },
  };
}
