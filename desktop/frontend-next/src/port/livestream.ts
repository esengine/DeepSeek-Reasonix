// The kernel restates its position on every transport once per
// SSEWatermarkInterval (15s, internal/frontend/serve/eventstream.go). Three
// silent intervals is a stream that is gone, whatever readyState says: a phone
// that slept or a Wi-Fi link that dropped leaves the socket looking open.
export const STREAM_SILENCE_MS = 45_000;
const CHECK_MS = 5_000;

/** Holds one EventSource open and replaces it once it has gone silent. `url`
 *  names where to resume, so a replacement asks for what the dead one missed.
 *  Timers stop while a phone sleeps, so waking and coming back online check at
 *  once rather than on the next tick. */
export function openLiveStream(url: () => string, onData: (raw: string) => void, silenceMs = STREAM_SILENCE_MS): () => void {
  let es: EventSource;
  let heard = 0;
  const open = () => {
    heard = Date.now();
    es = new EventSource(url(), { withCredentials: true });
    es.onopen = () => {
      heard = Date.now();
    };
    es.onmessage = (m) => {
      heard = Date.now();
      onData(m.data);
    };
  };
  const check = () => {
    if (Date.now() - heard < silenceMs) return;
    es.close();
    open();
  };
  const wake = () => {
    if (document.visibilityState === "visible") check();
  };
  open();
  const timer = setInterval(check, CHECK_MS);
  const dom = typeof document !== "undefined" && typeof addEventListener === "function";
  if (dom) {
    document.addEventListener("visibilitychange", wake);
    addEventListener("online", check);
  }
  return () => {
    clearInterval(timer);
    if (dom) {
      document.removeEventListener("visibilitychange", wake);
      removeEventListener("online", check);
    }
    es.close();
  };
}
