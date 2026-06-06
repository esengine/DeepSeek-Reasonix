import type { Card } from "./cards.js";
import type { AgentEvent } from "./events.js";
import { reduce } from "./reducer.js";
import { type AgentState, type SessionInfo, initialState } from "./state.js";

export type StateListener = () => void;
export type EventListener = (event: AgentEvent) => void;

export interface AgentStore {
  getState(): AgentState;
  dispatch(event: AgentEvent): void;
  subscribe(listener: StateListener): () => void;
  onEvent(listener: EventListener): () => void;
}

export function createStore(session: SessionInfo, initialCards?: ReadonlyArray<Card>): AgentStore {
  let state = initialState(session, initialCards);
  const stateListeners = new Set<StateListener>();
  const eventListeners = new Set<EventListener>();

  // Macrotask-batched notification: during rapid streaming, dozens of chunks arrive
  // within a single event-loop drain.  Each `dispatch()` used to synchronously
  // notify every listener, causing React's `useSyncExternalStore` to re-render
  // on *every* token.  When renders couldn't keep up, React hit "Maximum update
  // depth exceeded".  Deferring via setTimeout(0) coalesces all dispatches that
  // happen inside the same tick into a single notification burst.
  let notifyScheduled = false;
  const scheduleNotify = () => {
    if (notifyScheduled) return;
    notifyScheduled = true;
    // setTimeout(0) defers to the next macrotask, coalescing all synchronous
    // dispatches (e.g. rapid streaming chunks) into a single notification.
    setTimeout(() => {
      notifyScheduled = false;
      for (const listener of stateListeners) listener();
    }, 0);
  };

  return {
    getState() {
      return state;
    },
    dispatch(event) {
      state = reduce(state, event);
      scheduleNotify();
      for (const listener of eventListeners) listener(event);
    },
    subscribe(listener) {
      stateListeners.add(listener);
      return () => {
        stateListeners.delete(listener);
      };
    },
    onEvent(listener) {
      eventListeners.add(listener);
      return () => {
        eventListeners.delete(listener);
      };
    },
  };
}
