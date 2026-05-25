import React from "react";
import {
  type ChatScrollState,
  type ChatScrollStore,
  createChatScrollStore,
} from "./chat-scroll-store.js";

const Ctx = React.createContext<ChatScrollStore | null>(null);

export function ChatScrollProvider({
  children,
  wheelRows,
}: {
  children: React.ReactNode;
  wheelRows?: number;
}): React.ReactElement {
  const store = React.useMemo(() => createChatScrollStore({ wheelRows }), [wheelRows]);
  return <Ctx.Provider value={store}>{children}</Ctx.Provider>;
}

function useStore(): ChatScrollStore {
  const store = React.useContext(Ctx);
  if (!store) throw new Error("useChatScroll* must be used inside ChatScrollProvider");
  return store;
}

export function useChatScrollState<T>(selector: (s: ChatScrollState) => T): T {
  const store = useStore();
  const subscribe = React.useCallback((cb: () => void) => store.subscribe(cb), [store]);
  const getSnapshot = React.useCallback(() => selector(store.getState()), [store, selector]);
  return React.useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export function useChatScrollActions(): Pick<
  ChatScrollStore,
  | "scrollUp"
  | "scrollDown"
  | "scrollPageUp"
  | "scrollPageDown"
  | "scrollWheelUp"
  | "scrollWheelDown"
  | "jumpToBottom"
  | "setMaxScroll"
  | "setCardHeight"
  | "pruneCardHeights"
> {
  return useStore();
}
