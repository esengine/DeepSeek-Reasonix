import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { HistoryHit, HistoryIndex, HistorySearch, HubPort, TreeWorkspace } from "../port/hub";
import { useRailQuery } from "./railsearch";

const MIN_CHARS = 2;
const SETTLE_MS = 300;

type Reading = { for: string; answer: HistorySearch | "failed" };

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

/** One line when the index could not answer for every session it knows. Without
 *  it, a source it failed to read and a query that matched nothing reach this
 *  rail as the same empty list. */
function indexNote(index: HistoryIndex | undefined): { reason: "failed" | "building"; note: string } | null {
  if (!index?.partial) return null;
  if (index.reason === "failed") {
    return { reason: "failed", note: t("chat.some_history_could_not_be") };
  }
  return {
    reason: "building",
    note: index.total > 0
      ? t("chat.the_history_index_is_still_2", { indexed: index.indexed, total: index.total })
      : t("chat.the_history_index_is_still"),
  };
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
        (answer) => current && setRead({ for: needle, answer }),
        () => current && setRead({ for: needle, answer: "failed" }),
      );
    }, SETTLE_MS);
    return () => {
      current = false;
      window.clearTimeout(timer);
    };
  }, [asks, needle, hub]);

  if (!asks) return null;
  const answer = read?.for === needle ? read.answer : null;
  if (answer === null) return <div className="content-matches" role="status" data-state="reading">{t("chat.searching_conversation_text")}</div>;
  if (answer === "failed") return <div className="content-matches" role="status" data-state="failed">{t("chat.conversation_text_cannot_be_searched")}</div>;
  const note = indexNote(answer.index);
  if (answer.hits.length === 0) {
    return note
      ? <div className="content-matches" role="status" data-state="incomplete" data-reason={note.reason}>{note.note}</div>
      : null;
  }
  return (
    <section className="content-matches" aria-label={t("chat.matches_in_conversation_text")}>
      <h3>{t("chat.matches_in_conversation_text")}</h3>
      {note && <p className="content-matches-note" role="status" data-state="incomplete" data-reason={note.reason}>{note.note}</p>}
      <ul>
        {answer.hits.map((hit) => (
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
                aria-label={t("chat.reference_title_in_the_message", { title: titleOf(hit, tree) })} title={t("chat.quote_into_the_composer")}
                onClick={() => onReference(hit.id, titleOf(hit, tree))}>#</button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
