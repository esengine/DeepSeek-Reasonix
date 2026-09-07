// Why a reach is open, as a set rather than a sentence.
//
// The canonical fact is which causes are still open. A one-line reason is a
// projection of that set and nothing may read it back: recording the first
// cause a traversal happened to meet made the traversal's order the owner of
// the semantics, and 54 of 72 identity cells were carrying causes that were
// never written down.
//
// Two layers, kept apart because the earlier work proved they are not the same
// debt. A raw cause is what an axis observed. A source cause is what a fix
// would be written against — and a projection (an axis reaching an effect whose
// setup is open) has none of its own: its sources are inside what it projects.

// Declared, so the one line a human reads is chosen by a rule rather than by
// which edge the walk met first.
const PRECEDENCE = ["dep provenance", "setup open", "cleanup unreadable", "cleanup open",
  "mount-open", "unmount-open", "presence-opaque-local", "presence-unresolved"];

/** An open edge, as the thing a fix is written against. The site is where it
 *  was found; the trail that reached it differs per root and is not identity. */
const sourceOf = (edge) => ({ mechanism: edge.kind, source: edge.kind + ":" + (edge.site ?? edge.at ?? "?"), via: edge });

const dedupe = (sources) => {
  const m = new Map();
  for (const s of sources) m.set(s.source, s);
  return [...m.values()].sort((a, b) => a.source.localeCompare(b.source));
};

const order = (raw) => raw.sort((a, b) =>
  (PRECEDENCE.indexOf(a.kind) - PRECEDENCE.indexOf(b.kind)) || a.at.localeCompare(b.at));

/** Every reason a cell's reach into the effect graph is open. `opaqueSites` are
 *  the dependency lists on that reach which could not be read; they are the
 *  cell's own, so they are attributed once rather than per effect. */
function reachCauses(hit, effects, opaqueSites) {
  const raw = [];
  for (const [ek, edgeUnresolved] of hit) {
    const e = effects.get(ek);
    if (edgeUnresolved) {
      raw.push({ kind: "dep provenance", at: ek, projection: false,
        sources: dedupe([...opaqueSites].map((s) => ({ mechanism: "dep-provenance", source: "dep-provenance:" + s }))) });
    }
    if (e?.setup.open) {
      raw.push({ kind: "setup open", at: ek, projection: true, sources: dedupe((e.setup.openEdges ?? []).map(sourceOf)) });
    }
    if (e?.cleanup?.unresolved) {
      raw.push({ kind: "cleanup unreadable", at: ek, projection: false,
        sources: [{ mechanism: "cleanup-unreadable", source: "cleanup-unreadable:" + ek }] });
    } else if (e?.cleanup && e.cleanup.open) {
      raw.push({ kind: "cleanup open", at: ek, projection: true, sources: dedupe((e.cleanup.openEdges ?? []).map(sourceOf)) });
    }
  }
  return order(raw);
}

/** Every reason a cell reaching a component's presence is open: the effect that
 *  mount or unmount would run, and whether the presence path itself is readable.
 *  Those are different debts and are recorded as different causes. */
function presenceCauses(d, effects, why) {
  const raw = [];
  const e = d.by ? effects.get(d.by) : null;
  if (d.mountOpen) raw.push({ kind: "mount-open", at: d.id, projection: true, sources: dedupe((e?.setup.openEdges ?? []).map(sourceOf)) });
  if (d.unmountOpen) {
    raw.push({ kind: "unmount-open", at: d.id, projection: true,
      sources: e?.cleanup?.unresolved
        ? [{ mechanism: "cleanup-unreadable", source: "cleanup-unreadable:" + d.by }]
        : dedupe((e?.cleanup?.openEdges ?? []).map(sourceOf)) });
  }
  // A guard the walk can read is not a cause. An opaque local and an unresolved
  // hop are, and they are the presence path's own rather than the effect's.
  for (const m of ["opaque-local", "unresolved"]) {
    if (!String(why ?? "").includes(m)) continue;
    raw.push({ kind: "presence-" + m, at: d.id, projection: false,
      sources: [{ mechanism: "presence-" + m, source: "presence-" + m + ":" + d.id }] });
  }
  return order(raw);
}

/** The one line a human reads. Deterministic, and an input to nothing. */
const primaryReason = (raw, at) => (raw.length ? (at ?? raw[0].at) + " (" + raw[0].kind + ")" : null);

/** A projection whose sources are empty is a debt with nothing behind it: the
 *  ranking would carry it as a mechanism of its own, which is the error the
 *  reduction exists to prevent. Counted so it can be held at zero. */
const projectionsWithoutSource = (raw) => raw.filter((c) => c.projection && !c.sources.length).length;

export { PRECEDENCE, dedupe, presenceCauses, primaryReason, projectionsWithoutSource, reachCauses, sourceOf };
