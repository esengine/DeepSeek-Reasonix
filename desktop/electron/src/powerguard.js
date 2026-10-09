"use strict";

const POLL_MS = 5000;
const KIND = "prevent-app-suspension";
const PREF_KEY = "rx-keep-awake";
// Consecutive idle answers before the block is let go: about fifteen seconds,
// so a turn that ends and another that starts do not flap it.
const IDLE_PASSES = 3;
// Consecutive passes with no usable answer before the block is let go anyway:
// about a minute, so a kernel that never answers again cannot hold it forever.
const SILENT_PASSES = 12;

// The page stores the preference as "off" or "on"; anything else is the default.
function keepsAwake(prefs) {
  return prefs?.[PREF_KEY] !== "off";
}

// createPowerGuard holds one system-suspension block while the kernel reports a
// turn in flight. running resolves to { panes, unknown } or rejects/returns
// null when the kernel itself cannot answer. A pane count above zero takes the
// block at once; letting go needs IDLE_PASSES clean zeros in a row, and a pass
// that could not be answered neither takes it nor counts toward letting go.
function createPowerGuard({ blocker, running, enabled, setInterval: every = setInterval, clearInterval: stop = clearInterval }) {
  let held = null;
  let pass = 0;
  let timer = null;
  let closed = false;
  let idle = 0;
  let silent = 0;

  const hold = () => {
    if (held !== null && !blocker.isStarted(held)) held = null;
    if (held === null) held = blocker.start(KIND);
  };
  const release = () => {
    idle = 0;
    silent = 0;
    if (held === null) return;
    if (blocker.isStarted(held)) blocker.stop(held);
    held = null;
  };

  async function refresh() {
    if (closed) return;
    const mine = ++pass;
    if (!enabled()) {
      release();
      return;
    }
    let answer = null;
    try {
      answer = await running();
    } catch {
      answer = null;
    }
    if (mine !== pass || closed) return;
    const panes = typeof answer?.panes === "number" ? answer.panes : null;
    const unknown = Number(answer?.unknown) > 0;
    if (panes !== null && panes > 0) {
      idle = 0;
      silent = 0;
      hold();
    } else if (panes === 0 && !unknown) {
      silent = 0;
      if (held === null || ++idle >= IDLE_PASSES) release();
    } else if (held !== null && ++silent >= SILENT_PASSES) {
      release();
    }
  }

  return {
    refresh,
    held: () => held !== null,
    begin() {
      if (timer) return;
      timer = every(() => void refresh(), POLL_MS);
      timer.unref?.();
      void refresh();
    },
    close() {
      closed = true;
      pass++;
      if (timer) stop(timer);
      timer = null;
      release();
    },
  };
}

module.exports = { createPowerGuard, keepsAwake, PREF_KEY, POLL_MS, KIND, IDLE_PASSES, SILENT_PASSES };
