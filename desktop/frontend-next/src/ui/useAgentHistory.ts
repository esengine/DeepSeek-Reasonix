import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { AgentPort, HistoryMessage } from "../port/port";
import type { SubagentRun } from "../port/subagent";
import { reason } from "../i18n/kernel";

const PAGE = 100;
const POLL = 1500;

export function useAgentHistory(port: AgentPort, ref: string, shown: boolean, scroll?: RefObject<HTMLDivElement | null>) {
  const [messages, setMessages] = useState<HistoryMessage[]>([]);
  const [info, setInfo] = useState<SubagentRun>();
  const [failed, setFailed] = useState("");
  const [before, setBefore] = useState(0);
  const [loading, setLoading] = useState(false);
  const cursor = useRef({ ref: "", snapshot: "", next: -1, before: 0, busy: false, generation: 0 });
  const anchor = useRef<{ height: number; top: number } | null>(null);

  useLayoutEffect(() => {
    const element = scroll?.current;
    if (element && anchor.current) element.scrollTop = anchor.current.top + element.scrollHeight - anchor.current.height;
    anchor.current = null;
  }, [messages, scroll]);

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const state = cursor.current;
    state.generation++;
    setLoading(false);
    if (state.ref !== ref) {
      state.ref = ref; state.snapshot = ""; state.next = -1; state.before = 0; state.busy = false;
      setMessages([]); setInfo(undefined); setBefore(0); setLoading(false); setFailed("");
    }
    const read = async () => {
      const after = state.next;
      try {
        const page = await port.subagentTranscript(ref, after, PAGE);
        if (!active) return;
        if (after >= 0 && page.snapshot && state.snapshot && state.snapshot !== page.snapshot) {
          state.next = -1; state.generation++; state.busy = false; setLoading(false);
          void read(); return;
        }
        state.snapshot = page.snapshot ?? "";
        setInfo(page.run); setFailed("");
        if (after < 0 || page.after < after) {
          setMessages(page.messages); state.before = page.after; setBefore(page.after);
        } else if (page.messages.length) {
          setMessages(all => [...all, ...page.messages]);
        }
        state.next = page.next;
        if (page.live || page.next < page.total) timer = setTimeout(() => void read(), POLL);
      } catch (error) {
        if (active) { setFailed(reason(error)); timer = setTimeout(() => void read(), POLL); }
      }
    };
    if (shown) void read();
    return () => { active = false; state.generation++; state.busy = false; clearTimeout(timer); };
  }, [port, ref, shown]);

  const earlier = useCallback(async () => {
    const state = cursor.current;
    if (state.ref !== ref || state.busy || state.before === 0 || !shown) return;
    const generation = state.generation;
    const end = state.before;
    state.busy = true; setLoading(true);
    try {
      const page = await port.subagentTranscript(ref, Math.max(0, end - PAGE), Math.min(PAGE, end));
      if (state.ref !== ref || state.generation !== generation) return;
      if (page.next !== end || (page.snapshot && state.snapshot && page.snapshot !== state.snapshot)) {
        state.next = -1;
        const latest = await port.subagentTranscript(ref, -1, PAGE);
        if (state.ref !== ref || state.generation !== generation) return;
        state.snapshot = latest.snapshot ?? ""; state.next = latest.next; state.before = latest.after;
        setMessages(latest.messages); setInfo(latest.run); setBefore(latest.after); setFailed("");
        return;
      }
      const element = scroll?.current;
      if (element) anchor.current = { height: element.scrollHeight, top: element.scrollTop };
      setMessages(all => [...page.messages, ...all]);
      state.before = page.after; setBefore(page.after); setFailed("");
    } catch (error) {
      if (state.ref === ref && state.generation === generation) setFailed(reason(error));
    } finally {
      if (state.ref === ref && state.generation === generation) { state.busy = false; setLoading(false); }
    }
  }, [port, ref, shown, scroll]);
  return { messages, info, failed, before, loading, earlier };
}
