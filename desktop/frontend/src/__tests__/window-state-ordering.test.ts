// Run: pnpm test:window-state

import { createWindowStateSaver, type WindowStateSnapshot } from "../lib/windowState";

let failed = 0;

function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (!value) failed += 1;
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

let nativeState = { width: 900, height: 600, x: 10, y: 20, maximised: false };
let sizeReads = 0;
const firstPersist = deferred();
const persisted: WindowStateSnapshot[] = [];
const save = createWindowStateSaver(
  {
    async WindowGetSize() {
      sizeReads += 1;
      return { w: nativeState.width, h: nativeState.height };
    },
    async WindowGetPosition() {
      return { x: nativeState.x, y: nativeState.y };
    },
    async WindowIsMaximised() {
      return nativeState.maximised;
    },
  },
  async (state) => {
    persisted.push(state);
    if (persisted.length === 1) await firstPersist.promise;
  },
);

const oldSave = save();
while (persisted.length === 0) await Promise.resolve();

nativeState = { width: 1280, height: 800, x: 40, y: 50, maximised: true };
const newSave = save();
await Promise.resolve();
ok(sizeReads === 1, "a newer request waits for the in-flight capture and persistence");

firstPersist.resolve();
await Promise.all([oldSave, newSave]);
ok(persisted.length === 2, "both distinct observations are persisted");
ok(persisted[0].width === 900 && persisted[1].width === 1280, "the newest observation is written last");
ok(persisted[1].maximised, "the newest maximised state is preserved");

await save();
ok(persisted.length === 2, "an unchanged observation is deduplicated after persistence succeeds");

if (failed > 0) process.exit(1);
