import { useEffect, useState } from "react";
import { Loader2, AlertCircle, RotateCcw } from "lucide-react";

// StreamingIndicator is the small "thinking / stalled / error" affordance
// shown beneath an in-flight assistant bubble. It's a state machine driven
// by the streaming prop on the message plus a wall-clock timer that
// escalates to "stalled" when no new chunk has arrived in 6 seconds.
//
//   preparing   — turn started, no text yet (a model warm-up pause)
//   streaming   — text deltas arrived within the stall window
//   stalled     — no deltas for >6s while still streaming (model is
//                 thinking, network is slow, or the request dropped)
//   error       — turn ended with `e.err` (Message already renders the
//                 error notice, this is just a small retry hint)
//
// The indicator is purely visual — it doesn't talk to the controller. The
// parent (Message) re-renders on every controller dispatch, so the timer
// is reset implicitly when item.text grows. We track the last text length
// we saw and the timestamp; a re-render with a longer text resets both.
//
// The Retry button is rendered in the `error` state only and is a no-op
// stub — wiring the actual retry requires a controller-level re-submit
// of the previous turn, which is a separate concern. The button is
// there to signal the affordance; the controller integration is a
// follow-up that adds a ResumeFrom(offset) binding.
export type StreamingPhase = "preparing" | "streaming" | "stalled" | "error";

export function StreamingIndicator({
  text,
  streaming,
  errored,
  onRetry,
}: {
  text: string;
  streaming: boolean;
  errored: boolean;
  onRetry?: () => void;
}) {
  // lastLen / lastTick are the "I saw progress recently" markers. A render
  // where the text grew (lastLen < text.length) updates lastTick to now.
  // The stalled check is "now - lastTick > STALL_MS".
  const [lastLen, setLastLen] = useState(text.length);
  const [lastTick, setLastTick] = useState(() => Date.now());
  const [, force] = useState(0); // re-render trigger for the stall timer

  useEffect(() => {
    if (text.length > lastLen) {
      setLastLen(text.length);
      setLastTick(Date.now());
    }
  }, [text.length, lastLen]);

  // Heartbeat re-render: once per second while we're streaming, the
  // indicator re-evaluates stalled vs streaming. We don't use setInterval
  // (it drifts and survives unmount); a 1Hz setTimeout chain self-cancels
  // on unmount and on the streaming prop flipping false.
  useEffect(() => {
    if (!streaming) return;
    const id = setTimeout(() => force((n) => n + 1), 1000);
    return () => clearTimeout(id);
  }, [streaming, lastTick, lastLen, text.length]);

  if (errored) {
    return (
      <div className="streaming-indicator streaming-indicator--error" role="status">
        <AlertCircle size={11} />
        <span>Turn ended with an error</span>
        {onRetry && (
          <button type="button" className="streaming-indicator__retry" onClick={onRetry}>
            <RotateCcw size={11} />
            <span>Retry</span>
          </button>
        )}
      </div>
    );
  }
  if (!streaming) return null;

  const sinceTick = Date.now() - lastTick;
  const phase: StreamingPhase = text.length === 0 ? "preparing" : sinceTick > 6000 ? "stalled" : "streaming";
  return (
    <div className={`streaming-indicator streaming-indicator--${phase}`} role="status" aria-live="polite">
      <Loader2 size={11} className="streaming-indicator__spin" />
      <span>
        {phase === "preparing"
          ? "Preparing…"
          : phase === "stalled"
            ? "Still working… (no new tokens in a moment)"
            : "Streaming…"}
      </span>
    </div>
  );
}
