export type PromptInput = {
  id: string;
  label: string;
  defaultValue?: string;
  secret?: boolean;
};

type Listener = () => void;

let activePrompt: PromptInput | null = null;
const pendingResolvers = new Map<string, (text: string | null) => void>();
const listeners = new Set<Listener>();
let counter = 0;

function notify(): void {
  for (const fn of listeners) fn();
}

export function getActivePromptInput(): PromptInput | null {
  return activePrompt;
}

export function subscribePromptInput(fn: Listener): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

export function requestPromptInput(opts: {
  label: string;
  defaultValue?: string;
  secret?: boolean;
}): Promise<string | null> {
  counter += 1;
  const id = `prompt-${Date.now()}-${counter}`;
  activePrompt = {
    id,
    label: opts.label,
    defaultValue: opts.defaultValue,
    secret: opts.secret,
  };
  notify();
  return new Promise<string | null>((resolve) => {
    pendingResolvers.set(id, resolve);
  });
}

export function resolvePromptInput(id: string, text: string | null): void {
  const resolver = pendingResolvers.get(id);
  if (resolver) {
    pendingResolvers.delete(id);
    resolver(text);
  }
  if (activePrompt?.id === id) {
    activePrompt = null;
    notify();
  }
}

export function cancelAllPromptInputs(): void {
  for (const [id, resolver] of pendingResolvers) {
    resolver(null);
    pendingResolvers.delete(id);
  }
  if (activePrompt !== null) {
    activePrompt = null;
    notify();
  }
}
