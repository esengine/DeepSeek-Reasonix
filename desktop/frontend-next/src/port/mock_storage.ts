import type { StoragePlan, StorageQuery, StorageState } from "./storage";

// A C: drive with three gigabytes left and a session store that ate the rest —
// the situation the panel exists for, so the fixture shows it rather than a
// tidy empty install.
const VOL = { volume: "C:", volumeFree: 3_100_000_000, volumeTotal: 476_000_000_000 };
const ROAMING = "C:/Users/you/AppData/Roaming/reasonix";
const LOCAL = "C:/Users/you/AppData/Local/reasonix";

export function mockStorage(query?: StorageQuery): StorageState {
  const all = mockStorageAll();
  if (query?.layout) return { ...all, roots: all.roots.map((r) => ({ ...r, bytes: 0, files: 0, pending: true })) };
  if (query?.root) return { ...all, roots: all.roots.filter((r) => r.id === query.root) };
  return all;
}

function mockStorageAll(): StorageState {
  return {
    editable: true,
    roots: [
      { id: "state", dir: ROAMING, bytes: 6_800_000_000, files: 9_612, relocatable: true, ...VOL },
      { id: "cache", dir: LOCAL, bytes: 1_900_000_000, files: 42, relocatable: true, ...VOL },
      { id: "worktrees", dir: `${LOCAL}/worktrees`, bytes: 412_000_000, files: 1_204, relocatable: true, ...VOL },
      { id: "home", dir: ROAMING, bytes: 12_288, files: 3, relocatable: false, ...VOL },
      { id: "locks", dir: LOCAL, bytes: 0, files: 0, relocatable: false, missing: true },
    ],
  };
}

export function mockStoragePlan(root: string, dir: string): StoragePlan {
  return {
    root, from: ROAMING, to: dir, bytes: 6_800_000_000, files: 9_612,
    need: 7_068_435_456, free: 214_000_000_000, ok: true,
  };
}
