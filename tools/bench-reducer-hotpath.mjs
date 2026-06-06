// Microbench comparing OLD vs NEW reducer hot paths in isolation:
//   1. mutateCard: O(n) findIndex  vs  O(1) Map.get
//   2. plan.drop:  O(n²) nested scan  vs  O(n) single backward scan
//   3. appendCard elision: full re-scan  vs  cursor-advanced re-scan

const RECENT = 200;
const ELIDED_PREFIX = "[elided — older than the last ";

function stub(c) {
  if (c.kind === "tool" && typeof c.output === "string" && c.output.length > 4096
      && !c.output.startsWith(ELIDED_PREFIX)) {
    return { ...c, output: `${ELIDED_PREFIX}stubbed]` };
  }
  return c;
}

const isImmutable = (k) =>
  k === "user" || k === "plan" || k === "usage" || k === "ctx" ||
  k === "doctor" || k === "tip" || k === "live" || k === "memory" ||
  k === "search" || k === "error" || k === "warn" || k === "compaction";

function buildCards(n) {
  const cards = [];
  for (let i = 0; i < n; i++) {
    if (i % 3 === 0) cards.push({ kind: "user", id: `u-${i}`, ts: 0, text: "hi" });
    else if (i % 3 === 1) cards.push({ kind: "tool", id: `t-${i}`, ts: 0, name: "x", args: {}, output: "x".repeat(8000), done: true, elapsedMs: 1 });
    else cards.push({ kind: "plan", id: `p-${i}`, ts: 0, title: "p", steps: [], variant: "active" });
  }
  return cards;
}

function buildIndex(cards) {
  const m = new Map();
  for (let i = 0; i < cards.length; i++) m.set(cards[i].id, i);
  return m;
}

// ---------- mutateCard ----------
function mutateOld(cards, id, kind, patch) {
  const idx = cards.findIndex((c) => c.id === id && c.kind === kind);
  if (idx < 0) return cards;
  const next = cards.slice();
  next[idx] = patch(cards[idx]);
  return next;
}
function mutateNew(cards, index, id, kind, patch) {
  const idx = index.get(id);
  if (idx === undefined) return cards;
  const existing = cards[idx];
  if (!existing || existing.kind !== kind) return cards;
  const next = cards.slice();
  next[idx] = patch(existing);
  return next;
}

function benchMutate(N, hits) {
  const cards = buildCards(N);
  const index = buildIndex(cards);
  const targets = [];
  for (let i = 0; i < hits; i++) {
    const k = (i * 7919) % N;
    targets.push(cards[k]);
  }
  const patch = (c) => ({ ...c, output: c.output + "+" });

  const t0 = process.hrtime.bigint();
  let work = cards;
  for (const t of targets) work = mutateOld(work, t.id, t.kind, patch);
  const t1 = process.hrtime.bigint();

  work = cards;
  for (const t of targets) work = mutateNew(work, index, t.id, t.kind, patch);
  const t2 = process.hrtime.bigint();

  return {
    old_ms: Number(t1 - t0) / 1e6,
    new_ms: Number(t2 - t1) / 1e6,
  };
}

// ---------- plan.drop ----------
function planDropOld(cards) {
  let dropped = false;
  return cards.map((c, i) => {
    if (dropped) return c;
    if (c.kind !== "plan" || c.variant !== "active") return c;
    if (cards.slice(i + 1).some((cc) => cc.kind === "plan" && cc.variant === "active")) return c;
    dropped = true;
    return { ...c, variant: "replay" };
  });
}
function planDropNew(cards) {
  let lastActive = -1;
  for (let i = cards.length - 1; i >= 0; i--) {
    const c = cards[i];
    if (c.kind === "plan" && c.variant === "active") { lastActive = i; break; }
  }
  if (lastActive < 0) return cards;
  const next = cards.slice();
  next[lastActive] = { ...next[lastActive], variant: "replay" };
  return next;
}

function benchPlanDrop(N, iters) {
  const cards = buildCards(N);
  const t0 = process.hrtime.bigint();
  for (let i = 0; i < iters; i++) planDropOld(cards);
  const t1 = process.hrtime.bigint();
  for (let i = 0; i < iters; i++) planDropNew(cards);
  const t2 = process.hrtime.bigint();
  return { old_ms: Number(t1 - t0) / 1e6, new_ms: Number(t2 - t1) / 1e6 };
}

// ---------- elision rescan ----------
function elideOld(cards) {
  if (cards.length < RECENT) return cards;
  const cutoff = cards.length + 1 - RECENT;
  let next = null;
  for (let i = 0; i < cutoff; i++) {
    const c = cards[i];
    const s = stub(c);
    if (s === c) continue;
    if (next === null) next = cards.slice();
    next[i] = s;
  }
  return next ?? cards;
}
function elideNew(cards, cursor) {
  if (cards.length < RECENT) return { cards, cursor };
  const cutoff = cards.length + 1 - RECENT;
  let next = null;
  let nextCursor = cursor;
  for (let i = cursor; i < cutoff; i++) {
    const c = cards[i];
    const s = stub(c);
    if (s !== c) {
      if (next === null) next = cards.slice();
      next[i] = s;
      nextCursor = i + 1;
      continue;
    }
    if (isImmutable(c.kind)) { nextCursor = i + 1; continue; }
    break;
  }
  return { cards: next ?? cards, cursor: nextCursor };
}

function benchElide(N, appends) {
  // Simulate `appends` consecutive appendCards on a session that starts at N cards
  let cards = buildCards(N);
  const t0 = process.hrtime.bigint();
  for (let i = 0; i < appends; i++) {
    cards = elideOld(cards);
    cards = [...cards, { kind: "user", id: `n-${i}`, ts: 0, text: "x" }];
  }
  const t1 = process.hrtime.bigint();

  let cards2 = buildCards(N);
  let cursor = 0;
  for (let i = 0; i < appends; i++) {
    const r = elideNew(cards2, cursor);
    cards2 = [...r.cards, { kind: "user", id: `n-${i}`, ts: 0, text: "x" }];
    cursor = r.cursor;
  }
  const t2 = process.hrtime.bigint();
  return { old_ms: Number(t1 - t0) / 1e6, new_ms: Number(t2 - t1) / 1e6 };
}

const fmt = (n) => n.toFixed(2).padStart(8);
const pct = (o, n) => `${(((o - n) / o) * 100).toFixed(1)}% faster`;

console.log("\n# mutateCard (streaming chunk hot path)");
console.log("cards |   ops   |    old(ms)  |    new(ms)  | gain");
for (const [n, h] of [[100, 1000], [500, 1000], [1000, 1000], [2000, 1000]]) {
  const r = benchMutate(n, h);
  console.log(`${String(n).padStart(5)} | ${String(h).padStart(7)} |${fmt(r.old_ms)}     |${fmt(r.new_ms)}     | ${pct(r.old_ms, r.new_ms)}`);
}

console.log("\n# plan.drop");
console.log("cards |  iters  |    old(ms)  |    new(ms)  | gain");
for (const [n, it] of [[100, 1000], [500, 1000], [1000, 1000]]) {
  const r = benchPlanDrop(n, it);
  console.log(`${String(n).padStart(5)} | ${String(it).padStart(7)} |${fmt(r.old_ms)}     |${fmt(r.new_ms)}     | ${pct(r.old_ms, r.new_ms)}`);
}

console.log("\n# elideOldCardContent rescan over many consecutive appends");
console.log("startN| appends |    old(ms)  |    new(ms)  | gain");
for (const [n, a] of [[300, 500], [500, 500], [1000, 500]]) {
  const r = benchElide(n, a);
  console.log(`${String(n).padStart(5)} | ${String(a).padStart(7)} |${fmt(r.old_ms)}     |${fmt(r.new_ms)}     | ${pct(r.old_ms, r.new_ms)}`);
}
