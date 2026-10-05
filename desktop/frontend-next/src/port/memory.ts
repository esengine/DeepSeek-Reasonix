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
}

export interface MemoryCatalog {
  memories: MemoryEntry[];
  recallQuery: string;
  indexPath?: string;
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
}
