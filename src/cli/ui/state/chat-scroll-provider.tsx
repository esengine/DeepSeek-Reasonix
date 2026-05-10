// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import {
  type ChatScrollState,
  type ChatScrollStore,
  createChatScrollStore,
} from "./chat-scroll-store.js";

const Ctx = React.createContext<ChatScrollStore | null>(null);

export function ChatScrollProvider({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  const store = React.useMemo(() => createChatScrollStore(), []);
  return <Ctx.Provider value={store}>{children}</Ctx.Provider>;
}

function useStore(): ChatScrollStore {
  const s = React.useContext(Ctx);
  if (!s) throw new Error("useChatScroll* must be used inside ChatScrollProvider");
  return s;
}

/** Subscribes to a slice of scroll state — only re-renders when that slice changes. */
export function useChatScrollState<T>(selector: (s: ChatScrollState) => T): T {
  const store = useStore();
  const subscribe = React.useCallback((cb: () => void) => store.subscribe(cb), [store]);
  const getSnapshot = React.useCallback(() => selector(store.getState()), [store, selector]);
  return React.useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

/** Returns the action set — stable across renders, never triggers re-renders by itself. */
export function useChatScrollActions(): Pick<
  ChatScrollStore,
  "scrollUp" | "scrollDown" | "scrollPageUp" | "scrollPageDown" | "jumpToBottom" | "setMaxScroll"
> {
  return useStore();
}
