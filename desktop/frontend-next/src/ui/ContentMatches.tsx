import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { HistoryHit, HubPort, TreeWorkspace } from "../port/hub";
import { useRailQuery } from "./railsearch";

const MIN_CHARS = 2;
const SETTLE_MS = 300;

type Reading = { for: string; hits: HistoryHit[] | "failed" };

function titleOf(hit: HistoryHit, tree: TreeWorkspace[]): string {
  const listed = tree.flatMap((w) => w.sessions).find((s) => s.path === hit.path);
  return listed?.title || hit.title || listed?.name || hit.id;
}

function rootOf(hit: HistoryHit, tree: TreeWorkspace[]): string | undefined {
  for (const workspace of tree) {
    const session = workspace.sessions.find((s) => s.path === hit.path);
    if (session) return session.workspaceRoot || workspace.root;
  }
  return hit.workspace || undefined;
}

/** Conversations whose words, not names, match what the rail is searching for.
 *  The name filter still answers instantly; this adds what was said once the
 *  reader has paused. */
export function ContentMatches({ hub, tree, onOpen, onError, onReference }: {
  hub: Pick<HubPort, "searchHistory">;
  tree: TreeWorkspace[];
  onOpen: (req: { root?: string; sessionPath?: string }) => Promise<void>;
  onError: (e: unknown) => void;
  onReference?: (id: string, title: string) => void;
}) {
  const needle = useRailQuery();
  const [read, setRead] = useState<Reading | null>(null);
  const asks = needle.length >= MIN_CHARS;

  useEffect(() => {
    if (!asks) return;
    let current = true;
    const timer = window.setTimeout(() => {
      hub.searchHistory(needle).then(
        (hits) => current && setRead({ for: needle, hits }),
        () => current && setRead({ for: needle, hits: "failed" }),
      );
    }, SETTLE_MS);
    return () => {
      current = false;
      window.clearTimeout(timer);
    };
  }, [asks, needle, hub]);

  if (!asks) return null;
  const answered = read?.for === needle ? read.hits : null;
  if (answered === null) return <div className="content-matches" role="status" data-state="reading">{t("contentMatches.searchingConversationText")}</div>;
  if (answered === "failed") return <div className="content-matches" role="status" data-state="failed">{t("contentMatches.conversationTextCannotBeSearched")}</div>;
  if (answered.length === 0) return null;
  return (
    <section className="content-matches" aria-label={t("contentMatches.matchesInConversationText")}>
      <h3>{t("contentMatches.matchesInConversationText")}</h3>
      <ul>
        {answered.map((hit) => (
          <li key={hit.path}>
            <button
              type="button"
              data-action="session.open-match"
              data-target={hit.path}
              onClick={() => void onOpen({ root: rootOf(hit, tree), sessionPath: hit.path }).catch(onError)}
            >
              <b>{titleOf(hit, tree)}</b>
              <span>{hit.snippet}</span>
            </button>
            {onReference && (
              <button type="button" className="content-match-ref" data-action="session.reference-match" data-target={hit.path}
                aria-label={t("contentMatches.referenceInTheMessageBox", { title: titleOf(hit, tree) })} title={t("common.quoteIntoTheComposer")}
                onClick={() => onReference(hit.id, titleOf(hit, tree))}>#</button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
