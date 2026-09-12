import { useCallback, useMemo, useSyncExternalStore } from "react";
import type { ChatSource } from "../lib/chatViewSource";
import type { ChatScrollController } from "../lib/chatScrollController";
import { useT } from "../lib/i18n";
import { TurnNavigator, type TurnRailItem } from "./harness-chat/TurnNavigator";
import css from "./harness-chat/TurnNavigator.styles";
import "./harness-chat/TurnNavigator.css";

function Preview({ source, item }: { source: ChatSource; item: TurnRailItem }) {
  const subscribe = useCallback((notify: () => void) => {
    const user = source.subscribeNode(item.turn, notify);
    const answer = item.answerKey ? source.subscribeNode(item.answerKey, notify) : undefined;
    return () => { user(); answer?.(); };
  }, [source, item]);
  const snapshot = useCallback(() => {
    const user = source.getNodeSnapshot(item.turn);
    const answer = item.answerKey ? source.getNodeSnapshot(item.answerKey) : undefined;
    return JSON.stringify([user?.kind === "user" ? user.item.text.slice(0, 300) : "", answer?.kind === "assistant" ? answer.item.text.slice(0, 500) : ""]);
  }, [source, item]);
  const [prompt, response] = JSON.parse(useSyncExternalStore(subscribe, snapshot, snapshot)) as string[];
  return <><div className={css.previewPrompt}>{prompt || item.ordinal}</div><div className={css.previewResponse}>{response}</div></>;
}

export default function ChatTurnNavigator({ source, scroll }: { source: ChatSource; scroll: ChatScrollController }) {
  const t = useT();
  const order = useSyncExternalStore(source.subscribeOrder, source.getOrderSnapshot, source.getOrderSnapshot);
  const position = useSyncExternalStore(scroll.subscribe, scroll.getSnapshot, scroll.getSnapshot);
  const items = useMemo(() => {
    const turns: TurnRailItem[] = [];
    for (const key of order) {
      const node = source.getNodeSnapshot(key);
      if (node?.kind === "user") turns.push({ turn: key, ordinal: turns.length + 1, prompt: "", response: "", anchor: { kind: "loaded" } });
      else if (node?.kind === "assistant" && turns.length) turns[turns.length - 1].answerKey = key;
    }
    return turns;
  }, [order, source]);
  const navigate = useCallback((item: TurnRailItem) => scroll.jump(item.turn), [scroll]);
  const preview = useCallback((item: TurnRailItem) => <Preview key={item.turn} source={source} item={item} />, [source]);
  return <TurnNavigator items={items} activeTurn={position.activeKey || null} busyTurn={null} onNavigate={navigate} renderPreview={preview} t={t} />;
}
