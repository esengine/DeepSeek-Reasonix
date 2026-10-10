import { useCallback, useMemo, useRef, useState } from "react";
import type { AgentPort, Checkpoint } from "../port/port";
import type { Item } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { t } from "../i18n";
import type { Quote, ReplyActions } from "./cards/SayCard";

interface Inputs {
  port: AgentPort;
  items: Item[];
  checkpoints: Checkpoint[];
  running: boolean;
  model?: string;
  submit: (text: string) => Promise<boolean>;
  reloadSession: () => Promise<void>;
  onSettings: (section?: string) => void;
  onRunDetail: () => void;
  onError: (e: unknown) => void;
}

interface ReplyTurn {
  turn: number;
  text: string;
  hasLaterTurns: boolean;
}

function sameTurns(a: Map<string, ReplyTurn>, b: Map<string, ReplyTurn>) {
  if (a.size !== b.size) return false;
  for (const [id, x] of a) {
    const y = b.get(id);
    if (!y || x.turn !== y.turn || x.text !== y.text || x.hasLaterTurns !== y.hasLaterTurns) return false;
  }
  return true;
}

/** What a finished reply can be acted on with, and the draft signal a quote
 *  travels to the composer on. The transcript owns neither: the pane holds the
 *  session these read from, and the composer is where a quote has to land. */
export function useReplyActions({ port, items, checkpoints, running, model, submit, reloadSession, onSettings, onRunDetail, onError }: Inputs) {
  const [quote, setQuote] = useState<Quote>({ text: "", n: 0 });

  // Re-running a turn is a conversation rewind and then the same words again:
  // the transcript goes back, the files do not, and the reply that was there
  // stays in the history the rewind wrote. Scope is conversation for exactly
  // that reason — reverting the work too would be a far larger promise than
  // the word "regenerate" makes.
  const regenerate = useCallback(
    async (turn: number, text: string) => {
      const plan = await port.prepareRewind(turn, "conversation");
      if (!plan.canConversation) throw new Error(plan.disabledReason || t("这一轮无法重新生成"));
      await port.commitRewind(plan.planId);
      await reloadSession();
      await submit(text);
    },
    [port, submit, reloadSession],
  );

  // A reply belongs to the most recent user turn, including when that turn
  // produced several replies. A turn without a paired checkpoint must not
  // inherit the preceding turn's rewind target.
  const derived = useMemo(() => {
    const paired = pairCheckpoints(items, checkpoints);
    const turns = new Map<string, ReplyTurn>();
    let ask: ReplyTurn | undefined;
    for (const item of items) {
      if (item.t === "user" && !item.pending && !item.steer) {
        if (ask) ask.hasLaterTurns = true;
        const cp = paired.get(item.id);
        ask = cp ? { turn: cp.turn, text: item.text, hasLaterTurns: false } : undefined;
      } else if (item.t === "say" && ask) {
        turns.set(item.id, ask);
      }
    }
    return { paired, turns };
  }, [items, checkpoints]);

  // A streamed delta makes a new items array without moving this answer; the
  // previous map is kept while equal so settled rows holding `reply` do not
  // redraw once per delta.
  const kept = useRef(derived.turns);
  if (!sameTurns(kept.current, derived.turns)) kept.current = derived.turns;
  const replyTurns = kept.current;

  // Which reply is being quoted is the kernel's to say, so the turn its
  // checkpoint named travels with the text. A transcript rebuilt without
  // checkpoints has no turn to give and sends none rather than a guess.
  const latest = useRef({ items, paired: derived.paired });
  latest.current = { items, paired: derived.paired };
  const turnOf = useCallback((id: string) => {
    const { items: all, paired } = latest.current;
    const at = all.findIndex((i) => i.id === id);
    for (let i = at < 0 ? all.length - 1 : at; i >= 0; i--) {
      const item = all[i];
      if (item.t !== "user" || item.pending) continue;
      return paired.get(item.id)?.turn;
    }
    return undefined;
  }, []);

  const reply = useMemo<ReplyActions>(
    () => ({
      onQuote: (text: string, id: string) => setQuote((q) => ({ text, turn: turnOf(id), n: q.n + 1 })),
      canRegenerate: (id: string) => !running && replyTurns.has(id),
      hasLaterTurns: (id: string) => replyTurns.get(id)?.hasLaterTurns ?? false,
      onRegenerate: (id: string) => {
        if (running) return;
        const ask = replyTurns.get(id);
        if (ask) void regenerate(ask.turn, ask.text).catch(onError);
      },
      model,
      onConfigureModel: () => onSettings("model"),
      onRunDetail,
    }),
    [replyTurns, running, regenerate, model, onSettings, onRunDetail, turnOf, onError],
  );

  // Rewriting a message is the same act with different words: the turn goes
  // back and what the person now means goes out. The card asks for it by the
  // turn its own checkpoint named, so it cannot aim at a turn that moved.
  const onResend = useCallback((turn: number, text: string) => regenerate(turn, text), [regenerate]);

  return { quote, reply, onResend };
}
