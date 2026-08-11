export type PendingNavigationRequest<T extends object> = T & {
  seq: number;
  resolve: () => void;
};

export type NavigationCoalescingRefs<T extends object> = {
  seqRef: { current: number };
  runningRef: { current: boolean };
  pendingRef: { current: PendingNavigationRequest<T> | null };
};

export function enqueueNavigationRequest<T extends object>(
  refs: NavigationCoalescingRefs<T>,
  input: T,
  run: (request: PendingNavigationRequest<T>) => Promise<void>,
): Promise<void> {
  const seq = refs.seqRef.current + 1;
  refs.seqRef.current = seq;

  let resolve!: () => void;
  const promise = new Promise<void>((res) => {
    resolve = res;
  });
  const request: PendingNavigationRequest<T> = { ...input, seq, resolve };

  const start = (next: PendingNavigationRequest<T>) => {
    refs.runningRef.current = true;
    void (async () => {
      try {
        await run(next);
      } catch {
        // run() is expected to handle user-visible failures. The scheduler keeps
        // navigation promises non-rejecting so event handlers cannot leak
        // unhandled rejections.
      } finally {
        next.resolve();
        const pending = refs.pendingRef.current;
        if (pending) {
          refs.pendingRef.current = null;
          start(pending);
        } else {
          refs.runningRef.current = false;
        }
      }
    })();
  };

  if (refs.runningRef.current) {
    refs.pendingRef.current?.resolve();
    refs.pendingRef.current = request;
    return promise;
  }

  start(request);
  return promise;
}

export type PendingOpenTopicRequest = PendingNavigationRequest<{
  scope: string;
  workspaceRoot: string;
  topicId: string;
  sessionPath?: string;
}>;

export type OpenTopicRequestInput = Omit<PendingOpenTopicRequest, "seq" | "resolve">;

export type OpenTopicCoalescingRefs = NavigationCoalescingRefs<OpenTopicRequestInput>;

export function enqueueOpenTopicRequest(
  refs: OpenTopicCoalescingRefs,
  input: OpenTopicRequestInput,
  run: (request: PendingOpenTopicRequest) => Promise<void>,
): Promise<void> {
  return enqueueNavigationRequest(refs, input, run);
}
