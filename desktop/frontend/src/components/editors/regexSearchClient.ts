import {
  REGEX_SEARCH_TIMEOUT_MS,
  type RegexSearchRequest,
  type RegexSearchResponse,
} from "./codeSearch";

export interface RegexSearchWorker {
  onmessage: ((event: MessageEvent<RegexSearchResponse>) => void) | null;
  onerror: ((event: ErrorEvent) => void) | null;
  postMessage(request: RegexSearchRequest): void;
  terminate(): void;
}

export interface RegexSearchCallbacks {
  onResponse(response: RegexSearchResponse): void;
}

export interface RegexSearchTaskOptions {
  createWorker?: () => Promise<RegexSearchWorker>;
  timeoutMs?: number;
  setTimer?: (callback: () => void, delay: number) => number;
  clearTimer?: (timer: number) => void;
}

export function startRegexSearch(
  request: RegexSearchRequest,
  callbacks: RegexSearchCallbacks,
  options: RegexSearchTaskOptions = {},
): () => void {
  const createWorker = options.createWorker ?? createInlineRegexWorker;
  const setTimer = options.setTimer ?? ((callback, delay) => window.setTimeout(callback, delay));
  const clearTimer = options.clearTimer ?? ((timer) => window.clearTimeout(timer));
  const timeoutMs = options.timeoutMs ?? REGEX_SEARCH_TIMEOUT_MS;
  let worker: RegexSearchWorker | null = null;
  let timer: number | null = null;
  let stopped = false;

  const stop = () => {
    if (timer != null) {
      clearTimer(timer);
      timer = null;
    }
    worker?.terminate();
    worker = null;
  };

  const finish = (response: RegexSearchResponse) => {
    if (stopped) return;
    stopped = true;
    stop();
    callbacks.onResponse(response);
  };

  // Start the deadline before the async module/Worker creation. A stalled
  // worker chunk must not leave the editor in a permanent pending state.
  timer = setTimer(() => finish({
    requestId: request.requestId,
    ok: false,
    error: "timeout",
  }), timeoutMs);

  void createWorker()
    .then((createdWorker) => {
      if (stopped) {
        createdWorker.terminate();
        return;
      }
      worker = createdWorker;
      worker.onmessage = (event) => finish(event.data);
      worker.onerror = () => finish({
        requestId: request.requestId,
        ok: false,
        error: "unavailable",
      });
      worker.postMessage(request);
    })
    .catch(() => finish({
      requestId: request.requestId,
      ok: false,
      error: "unavailable",
    }));

  return () => {
    if (stopped) return;
    stopped = true;
    stop();
  };
}

async function createInlineRegexWorker(): Promise<RegexSearchWorker> {
  const { default: RegexSearchWorkerConstructor } = await import("./regexSearch.worker?worker&inline");
  return new RegexSearchWorkerConstructor();
}
