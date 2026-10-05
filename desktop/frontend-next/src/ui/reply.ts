import { useCallback, useMemo, useState } from "react";
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
      try {
        const plan = await port.prepareRewind(turn, "conversation");
        if (!plan.canConversation) throw new Error(plan.disabledReason || t("这一轮无法重新生成"));
        await port.commitRewind(plan.planId);
        await reloadSession();
        await submit(text);
      } catch (e) {
        onError(e);
      }
    },
    [port, submit, reloadSession, onError],
  );

  // A reply belongs to the most recent user turn, including when that turn
  // produced several replies. A turn without a paired checkpoint must not
  // inherit the preceding turn's rewind target.
  const replyTurns = useMemo(() => {
    const paired = pairCheckpoints(items, checkpoints);
    const turns = new Map<string, { turn: number; text: string; hasLaterTurns: boolean }>();
    let ask: { turn: number; text: string; hasLaterTurns: boolean } | undefined;
    for (const item of items) {
      if (item.t === "user" && !item.pending && !item.steer) {
        if (ask) ask.hasLaterTurns = true;
        const cp = paired.get(item.id);
        ask = cp ? { turn: cp.turn, text: item.text, hasLaterTurns: false } : undefined;
      } else if (item.t === "say" && ask) {
        turns.set(item.id, ask);
      }
    }
    return turns;
  }, [items, checkpoints]);

  // Which reply is being quoted is the kernel's to say, so the turn its
  // checkpoint named travels with the text. A transcript rebuilt without
  // checkpoints has no turn to give and sends none rather than a guess.
  const turnOf = useCallback(
    (id: string) => {
      const paired = pairCheckpoints(items, checkpoints);
      const at = items.findIndex((i) => i.id === id);
      for (let i = at < 0 ? items.length - 1 : at; i >= 0; i--) {
        const item = items[i];
        if (item.t !== "user" || item.pending) continue;
        return paired.get(item.id)?.turn;
      }
      return undefined;
    },
    [items, checkpoints],
  );

  const reply = useMemo<ReplyActions>(
    () => ({
      onQuote: (text: string, id: string) => setQuote((q) => ({ text, turn: turnOf(id), n: q.n + 1 })),
      canRegenerate: (id: string) => !running && replyTurns.has(id),
      hasLaterTurns: (id: string) => replyTurns.get(id)?.hasLaterTurns ?? false,
      onRegenerate: (id: string) => {
        if (running) return;
        const ask = replyTurns.get(id);
        if (ask) void regenerate(ask.turn, ask.text);
      },
      model,
      onConfigureModel: () => onSettings("model"),
      onRunDetail,
    }),
    [replyTurns, running, regenerate, model, onSettings, onRunDetail, turnOf],
  );

  // Rewriting a message is the same act with different words: the turn goes
  // back and what the person now means goes out. The card asks for it by the
  // turn its own checkpoint named, so it cannot aim at a turn that moved.
  const onResend = useCallback((turn: number, text: string) => regenerate(turn, text), [regenerate]);

  return { quote, reply, onResend };
}
