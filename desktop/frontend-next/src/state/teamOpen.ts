import type { RuntimeView } from "../port/hub";

// How a view inside a pane opens an agent's conversation: the window's team
// layer registers the one way it adopts a pane, so a mention in a message opens
// the agent exactly as its sidebar row does.
let opener: ((agent: string) => Promise<RuntimeView | void>) | null = null;

export function setAgentOpener(fn: typeof opener) {
  opener = fn;
}

export function openAgentPane(agent: string): Promise<RuntimeView | void> {
  return opener ? opener(agent) : Promise.resolve();
}
