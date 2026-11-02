import { useEffect, useSyncExternalStore } from "react";
import type { HubPort } from "../port/hub";
import type { TeamPort, TeamView } from "../port/team";

// The team as the kernel last answered it, one store per hub. It is read again
// on every tree change, which the kernel sends when the roster, a round or an
// agent's turn moves, so nothing here decides when the team is stale.
export interface TeamState {
  view: TeamView | null;
  // The kernel has no team to offer: an older one, or a page with it off.
  unavailable: boolean;
  error: unknown;
}

class TeamStore {
  private state: TeamState = { view: null, unavailable: false, error: null };
  private readonly listeners = new Set<() => void>();
  private inflight: Promise<void> | null = null;
  private again = false;
  private users = 0;
  private unwatch: (() => void) | null = null;

  constructor(private readonly hub: HubPort) {}

  get = () => this.state;

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => void this.listeners.delete(listener);
  };

  private set(next: TeamState) {
    this.state = next;
    for (const l of this.listeners) l();
  }

  port(): TeamPort | null {
    return this.hub.team();
  }

  // One read at a time; a change that lands during a read asks for one more.
  refresh = (): Promise<void> => {
    if (this.inflight) {
      this.again = true;
      return this.inflight;
    }
    const team = this.port();
    if (!team) {
      this.set({ view: null, unavailable: true, error: null });
      return Promise.resolve();
    }
    this.inflight = team.view().then(
      (view) => this.set({ view, unavailable: view === null, error: null }),
      (error) => this.set({ ...this.state, error }),
    ).finally(() => {
      this.inflight = null;
      if (this.again) {
        this.again = false;
        void this.refresh();
      }
    });
    return this.inflight;
  };

  // Shown takes the store's answer straight from a write, so the window does
  // not wait for the change notice to see what it just did.
  shown = (view: TeamView) => this.set({ view, unavailable: false, error: null });

  attach() {
    current = this;
    for (const l of currentListeners) l();
    if (this.users++ === 0) {
      this.unwatch = this.hub.onTreeChanged(() => void this.refresh());
      void this.refresh();
    }
    return () => {
      if (--this.users === 0) {
        this.unwatch?.();
        this.unwatch = null;
        if (current === this) {
          current = null;
          for (const l of currentListeners) l();
        }
      }
    };
  }
}

const stores = new WeakMap<HubPort, TeamStore>();

// The store a window most recently attached, for views that draw an agent's
// name without holding the hub: a transcript row, a card.
let current: TeamStore | null = null;
const currentListeners = new Set<() => void>();

/** The team the window last attached, for a view drawn inside a pane. */
export function useCurrentTeam(): { view: TeamView | null; team: TeamPort | null; shown: (view: TeamView) => void } {
  const view = useSyncExternalStore(
    (l) => {
      currentListeners.add(l);
      const off = current?.subscribe(l);
      return () => {
        currentListeners.delete(l);
        off?.();
      };
    },
    () => current?.get().view ?? null,
  );
  return { view, team: current?.port() ?? null, shown: (next) => current?.shown(next) };
}

/** The name the team knows an agent by, or undefined when it does not. */
export function useAgentName(id: string | undefined): string | undefined {
  const { view } = useCurrentTeam();
  return id ? view?.agents.find((a) => a.id === id)?.name : undefined;
}

export function teamStore(hub: HubPort): TeamStore {
  let store = stores.get(hub);
  if (!store) {
    store = new TeamStore(hub);
    stores.set(hub, store);
  }
  return store;
}

export function useTeam(hub: HubPort) {
  const store = teamStore(hub);
  useEffect(() => store.attach(), [store]);
  const state = useSyncExternalStore(store.subscribe, store.get);
  return { ...state, team: store.port(), refresh: store.refresh, shown: store.shown };
}
