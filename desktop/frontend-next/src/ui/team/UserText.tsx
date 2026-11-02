import { Fragment } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import { useCurrentTeam } from "../../state/team";
import { openAgentPane } from "../../state/teamOpen";
import { pushToast } from "../../state/toasts";

// A reference token starts the line or follows whitespace, as the kernel reads
// one, so an address like a@b.com is never taken for a mention.
const TOKEN = /(^|\s)@(agent:[a-z0-9-]+|[^\s@]+)/g;

// What the person wrote, with the team's references drawn as what they are: a
// teammate mentioned by name opens its conversation, and a teammate's quoted
// conversation names whose it is. Off the team the text reads as written.
export function UserText({ text }: { text: string }) {
  const { view } = useCurrentTeam();
  if (!view) return <div className="txt" dir="auto">{text}</div>;
  const byName = new Map(view.agents.map((a) => [a.name, a]));
  const byId = new Map(view.agents.map((a) => [a.id, a]));
  const parts: React.ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(TOKEN)) {
    const start = m.index + m[1].length;
    const word = m[2];
    const quoted = word.startsWith("agent:") ? byId.get(word.slice("agent:".length)) : undefined;
    const named = byName.get(word);
    if (!quoted && !named) continue;
    parts.push(text.slice(last, start));
    if (quoted) {
      parts.push(<span key={start} className="team-ref-chip" data-kind="agent-chat">{t("{name} 的对话", { name: quoted.name })}</span>);
    } else if (named) {
      parts.push(
        <button key={start} type="button" className="team-mention" data-action="team.mention-open" data-target={named.id}
          onClick={() => void openAgentPane(named.id).catch((e) => pushToast(reason(e)))}>
          {"@" + named.name}
        </button>,
      );
    }
    last = start + 1 + word.length;
  }
  parts.push(text.slice(last));
  return <div className="txt" dir="auto">{parts.map((p, i) => <Fragment key={i}>{p}</Fragment>)}</div>;
}
