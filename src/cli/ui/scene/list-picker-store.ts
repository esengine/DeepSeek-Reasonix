export type ListPickerOption = {
  key: string;
  label: string;
  sublabel?: string;
  meta?: string;
};

export type ListPicker = {
  id: string;
  title: string;
  hint?: string;
  options: ListPickerOption[];
};

type Listener = () => void;

let active: ListPicker | null = null;
const resolvers = new Map<string, (key: string | null) => void>();
const listeners = new Set<Listener>();
let counter = 0;

function notify(): void {
  for (const fn of listeners) fn();
}

export function getActiveListPicker(): ListPicker | null {
  return active;
}

export function subscribeListPicker(fn: Listener): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function requestListPicker(opts: {
  title: string;
  hint?: string;
  options: ListPickerOption[];
}): Promise<string | null> {
  counter += 1;
  const id = `list-${Date.now()}-${counter}`;
  active = {
    id,
    title: opts.title,
    hint: opts.hint,
    options: opts.options,
  };
  notify();
  return new Promise<string | null>((resolve) => {
    resolvers.set(id, resolve);
  });
}

export function resolveListPicker(id: string, key: string | null): void {
  const resolver = resolvers.get(id);
  if (resolver) {
    resolvers.delete(id);
    resolver(key);
  }
  if (active?.id === id) {
    active = null;
    notify();
  }
}

export function cancelAllListPickers(): void {
  for (const [id, resolver] of resolvers) {
    resolver(null);
    resolvers.delete(id);
  }
  if (active !== null) {
    active = null;
    notify();
  }
}
