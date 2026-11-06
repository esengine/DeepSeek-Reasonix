// Facts the agent wrote down about this project, and whether the last turn
// actually pulled one back.
// One saved fact. activation is the field a reader needs most and the type does
// not imply it: pinned facts sit in every prompt, relevant ones only surface
// when the turn looks related. usedLastTurn ties a stored fact to the behaviour
// the user just watched — the question they actually have.
export interface MemoryEntry {
  name: string;
  title?: string;
  description?: string;
  body?: string;
  type?: string;
  scope?: string;
  activation: string;
  path?: string;
  revision?: number;
  createdAt?: string;
  updatedAt?: string;
  expired?: boolean;
  usedLastTurn?: boolean;
  why?: string;
  galaxy?: string;
  importance?: number;
  state?: "clear" | "fuzzy" | "faded";
  uses?: number;
  lastUsedAt?: string;
  clearForDays?: number;
  noDecay?: boolean;
  new?: boolean;
}

// A memory file that is present but could not be read. It is reported beside
// the catalog so the panel can say the fact exists instead of showing a store
// that looks like it never held it.
export interface UnreadableMemory {
  name: string;
  path: string;
  error: string;
}

// What the model may do on its own with memory: nothing, ask first, or write.
export type MemoryProactive = "off" | "ask" | "auto";

// A fact the model proposed and the user has not decided on.
export interface MemorySuggestion {
  id: string;
  name: string;
  title?: string;
  description: string;
  body: string;
  type?: string;
  scope?: string;
}

// What the model may propose to keep, and the user's say over it.
export interface MemoryHoldPort {
  memoryProactive(): Promise<MemoryProactive>;
  setMemoryProactive(mode: MemoryProactive): Promise<void>;
  memorySuggestions(): Promise<MemorySuggestion[]>;
  /** Saves a held suggestion; fields given replace the model's wording. */
  acceptMemorySuggestion(id: string, edit?: { title?: string; description?: string; body?: string }): Promise<void>;
  dismissMemorySuggestion(id: string): Promise<void>;
}

export interface MemoryCatalog {
  memories: MemoryEntry[];
  recallQuery: string;
  indexPath?: string;
  unreadable?: UnreadableMemory[];
}

// What a person may change about a saved fact. Identity, revision and
// timestamps are the store's — the panel never sends them back.
export interface MemoryEdit {
  name: string;
  title: string;
  description: string;
  body: string;
  activation: string;
  keywords?: string;
  galaxy?: string;
  importance?: number;
  type?: string;
}
