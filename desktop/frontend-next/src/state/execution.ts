import type { ExecutionGraphRead, ExecutionInterruption, GraphDelta, WireEvent } from "../port/wire";
import { foldDelta, graphOf, initialGraph, type GraphState } from "./graph";

// Where this model stands relative to its authority. Only "live" folds a delta
// as it arrives: while a snapshot is being read, a delta describes a state the
// answer in flight is about to replace.
export type ExecutionPhase = "loading" | "live" | "resyncing";

// What last moved the state. A node that appeared because a snapshot replaced
// the model was not just spawned, and a view keyed on insertion would say it was.
export type ExecutionOrigin = "reset" | "snapshot" | "delta";

export interface ExecutionState {
  phase: ExecutionPhase;
  origin: ExecutionOrigin;
  // Which conversation this describes, as the snapshot named it. Empty until one
  // has been read: frame numbers cross a session switch and say nothing about
  // which run they belong to.
  sessionId: string;
  graph: GraphState;
  // Neither of these has a delta. The snapshot is their only source, which is
  // why it replaces the model rather than merging into it.
  interruptions: ExecutionInterruption[];
  identityUnknown: string[];
}

export const initialExecution: ExecutionState = {
  phase: "loading",
  origin: "reset",
  sessionId: "",
  graph: initialGraph,
  interruptions: [],
  identityUnknown: [],
};

/** ExecutionStore is the run graph a view reads, kept the way the kernel says it
 *  stands: a snapshot is the authority and replaces the state, deltas are the
 *  same facts arriving sooner and fold onto it. It never replays a delta stream
 *  to rebuild — the numbers those frames carry belong to the transport, and the
 *  only thing this holds against them is the cut one snapshot was read at. */
export class ExecutionStore {
  private state = initialExecution;
  private readers = new Set<() => void>();
  // Bumped by every session change. A read carries the number it began under, so
  // an answer for a conversation this store has left cannot be adopted by the one
  // it is showing now — which "the newest answer wins" would do.
  private generation = 0;
  // Deltas that arrived while the authority was being read. They are not dropped:
  // the ones after the cut are the only record of what happened since.
  private waiting: { seq?: number; delta: GraphDelta }[] = [];

  read = (): ExecutionState => this.state;

  subscribe = (onChange: () => void) => {
    this.readers.add(onChange);
    return () => {
      this.readers.delete(onChange);
    };
  };

  /** resetForSession drops everything and waits for the next authority. Holding
   *  the old graph while the new one loads would show one conversation's work
   *  under another's name for as long as the read takes. */
  resetForSession() {
    this.generation += 1;
    this.waiting = [];
    this.put({ ...initialExecution, phase: "loading", origin: "reset" });
  }

  /** onEvent is the transport's whole vocabulary here: one kind of frame moves
   *  this model, and the rest belong to other readers of the same stream. */
  onEvent(ev: WireEvent) {
    if (ev.kind !== "graph_delta" || !ev.graph) return;
    this.applyDelta(ev.graph, ev.seq);
  }

  /** applyDelta folds one publication. Nodes and edges only: interruptions and
   *  unrecorded identities have no delta, so a stream cannot contradict the
   *  snapshot about them. */
  applyDelta(delta: GraphDelta, seq?: number) {
    if (this.state.phase !== "live") {
      this.waiting.push({ seq, delta });
      return;
    }
    const graph = foldDelta(this.state.graph, delta);
    if (graph === this.state.graph) return;
    this.put({ ...this.state, graph, origin: "delta" });
  }

  /** replaceSnapshot takes the answer as the whole model. Replace, not merge:
   *  the two lists beside the graph shrink as well as grow, and a merge would
   *  keep an interruption the kernel has since resolved. */
  replaceSnapshot(view: ExecutionGraphRead) {
    this.put({
      phase: "live",
      origin: "snapshot",
      sessionId: view.sessionId ?? "",
      graph: graphOf(view.graph ?? {}),
      interruptions: view.interruptions ?? [],
      identityUnknown: view.identityUnknown ?? [],
    });
  }

  /** bootstrap reads the authority for the first time and answers with the frame
   *  it is at least as new as, which is what the transport resumes the numbered
   *  stream after. It refuses rather than answering zero: a caller told the read
   *  succeeded would resume from the start of the log. */
  bootstrap(read: () => Promise<ExecutionGraphRead>): Promise<number> {
    return this.resync(read, "loading");
  }

  /** recoverFromGap is the same read with the stream still running. The frames
   *  the transport could not replay are gone for good, so the model goes back to
   *  the authority — and holds what arrives meanwhile, because those deltas
   *  describe a state this is about to replace. */
  async recoverFromGap(read: () => Promise<ExecutionGraphRead>) {
    // The caller already knows the stream broke; a failed re-read adds nothing
    // it can act on, and the next gap or session change asks again.
    await this.resync(read, "resyncing").catch(() => {});
  }

  private async resync(read: () => Promise<ExecutionGraphRead>, phase: ExecutionPhase): Promise<number> {
    const mine = this.generation;
    const from = this.state.sessionId;
    if (this.state.phase !== phase) this.put({ ...this.state, phase });
    let view: ExecutionGraphRead;
    try {
      view = await read();
    } catch (err) {
      // No answer to replace the state with. What was held is all this model
      // has, so it folds that and goes back to running rather than waiting on
      // something that is not coming.
      if (mine === this.generation) {
        this.put({ ...this.state, phase: "live" });
        this.drain();
      }
      throw err;
    }
    // A session change while this was in flight owns the store now, and this
    // answer describes a conversation the view has left. The number is still a
    // real position in the stream, so the transport may resume from it.
    if (mine !== this.generation) return view.watermark;
    const foreign = !!from && !!view.sessionId && view.sessionId !== from;
    this.replaceSnapshot(view);
    this.drain(view.watermark, foreign);
    return view.watermark;
  }

  // What was held while the authority was read. A delta the snapshot already
  // accounts for is dropped rather than folded — an older publication would take
  // a settled node back to what it said on the way there — and a whole buffer is
  // dropped when the answer names a different conversation than it was collected
  // under, because then none of it describes what is now on screen.
  private drain(cut = 0, foreign = false) {
    const held = this.waiting;
    this.waiting = [];
    if (foreign) return;
    for (const w of held) {
      if (w.seq && w.seq <= cut) continue;
      this.applyDelta(w.delta, w.seq);
    }
  }

  private put(next: ExecutionState) {
    this.state = next;
    for (const reader of this.readers) reader();
  }
}
