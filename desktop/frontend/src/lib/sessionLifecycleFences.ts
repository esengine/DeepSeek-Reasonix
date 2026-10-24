import type { ProjectNode } from "./types";
import { canonicalProjectSession, projectSessionIdentity, projectSessionKeys } from "./projectSessionIdentity";

type Fence = { generation: number; operationId: string; aliases: readonly string[]; canonical?: string };

/** Application-owned: remounting the sidebar cannot revive a committed archive.
 * An active directory row with a newer lifecycle proves explicit restoration.
 * Runtime revisions cannot establish that proof and never release this fence.
 */
export function createSessionLifecycleFences() {
  const fences = new Map<string, Fence>();
  return {
    archive(node: ProjectNode, receipt: { lifecycleGeneration: number; operationId: string; identityAliases?: string[] }) {
      const aliases = [...new Set([...projectSessionKeys(node), ...(receipt.identityAliases ?? [])])];
      const canonical = canonicalProjectSession(node);
      const owners = canonical ? [canonical] : aliases.filter(key => key.startsWith("ref\u0000"));
      // A source archive may adopt one or more canonical targets. Retain each
      // receipt owner separately so restoring one cannot release its siblings.
      for (const key of owners.length ? owners : [projectSessionIdentity(node)]) {
        const fence = { generation: receipt.lifecycleGeneration, operationId: receipt.operationId,
          canonical: owners.length ? key : undefined,
          aliases: aliases.filter(alias => !alias.startsWith("ref\u0000") || alias === key) };
        if ((fences.get(key)?.generation ?? -1) <= fence.generation) fences.set(key, fence);
      }
    },
    observeDirectory(nodes: readonly ProjectNode[]) {
      const visit = (node: ProjectNode) => {
        const canonical = canonicalProjectSession(node);
        for (const key of canonical ? [canonical] : projectSessionKeys(node)) {
          const fence = fences.get(key);
          if (fence && (!fence.canonical || fence.canonical === canonical) && !node.runtimeOnly && (node.lifecycleGeneration ?? 0) > fence.generation) {
            fences.delete(key);
          }
        }
        node.children?.forEach(visit);
      };
      nodes.forEach(visit);
    },
    keys(): ReadonlySet<string> { return new Set([...fences.values()].flatMap(fence => fence.aliases)); },
  };
}

export const sessionLifecycleFences = createSessionLifecycleFences();
