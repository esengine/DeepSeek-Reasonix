export interface PollClock {
  schedule(callback: () => void, delay: number): unknown;
  cancel(handle: unknown): void;
}

export function createSerialWorkspacePoll(clock: PollClock, interval = 5000) {
  let job: (() => Promise<void>) | null = null;
  let timer: unknown = null;
  let running = false;
  let pending = false;
  const clear = () => {
    if (timer !== null) clock.cancel(timer);
    timer = null;
  };
  const run = () => {
    clear();
    if (!job) return;
    if (running) { pending = true; return; }
    const current = job;
    running = true;
    pending = false;
    void Promise.resolve().then(() => job === current ? current() : undefined).catch(() => {}).finally(() => {
      running = false;
      if (!job) return;
      if (pending || job !== current) run();
      else timer = clock.schedule(run, interval);
    });
  };
  return {
    setJob(next: (() => Promise<void>) | null) {
      clear();
      job = next;
      pending = false;
      if (job) run();
    },
    refresh: run,
  };
}
